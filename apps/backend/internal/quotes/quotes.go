// Package quotes keeps the daily closing prices that give holdings a worth in NT$ (ADR 0017), as prices
// of the securities some account holds or held (ADR 0020), and each security's code and market.
package quotes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/domain"
	"github.com/hsin19/finchie/apps/backend/internal/jobs"
	"github.com/hsin19/finchie/apps/backend/internal/ledger"
	"github.com/hsin19/finchie/apps/backend/internal/taipei"
)

// Feed is one exchange's daily list of closing prices: free, without a key.
type Feed struct {
	Market string
	URL    string
}

// Feeds are the listed (TWSE) and the OTC (TPEx) securities' lists.
var Feeds = []Feed{
	{Market: "twse", URL: "https://openapi.twse.com.tw/v1/exchangeReport/STOCK_DAY_ALL"},
	{Market: "tpex", URL: "https://www.tpex.org.tw/openapi/v1/tpex_mainboard_daily_close_quotes"},
}

// FetchArgs is the periodic job that keeps the newest closes.
type FetchArgs struct{}

func (FetchArgs) Kind() string { return "fetch_quotes" }

func (FetchArgs) InsertOpts() river.InsertOpts { return jobs.SourceOpts() }

// Periodic reads the closes as a source (jobs.Source).
func Periodic() *river.PeriodicJob { return jobs.Source("fetch_quotes", FetchArgs{}) }

type Worker struct {
	river.WorkerDefaults[FetchArgs]
	pool   *pgxpool.Pool
	client *http.Client
	feeds  []Feed
}

func NewWorker(pool *pgxpool.Pool, client *http.Client, feeds []Feed) *Worker {
	return &Worker{pool: pool, client: client, feeds: feeds}
}

func (w *Worker) Timeout(*river.Job[FetchArgs]) time.Duration { return 5 * time.Minute }

// Work keeps the closes of the securities some account holds or held; with none, it asks for nothing.
// One exchange failing does not lose the other's closes, but the job fails so River retries it.
func (w *Worker) Work(ctx context.Context, _ *river.Job[FetchArgs]) error {
	queries := dbq.New(w.pool)
	names, err := queries.Securities(ctx)
	if err != nil {
		return fmt.Errorf("securities: %w", err)
	}
	if len(names) == 0 {
		return nil
	}
	held := make(map[string]bool, len(names))
	for _, name := range names {
		held[name] = true
	}
	var errs []error
	for _, feed := range w.feeds {
		rows, err := w.fetch(ctx, feed.URL)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", feed.URL, err))
			continue
		}
		codes, prices := keep(feed.Market, rows, held)
		if len(prices) == 0 {
			continue
		}
		if err := queries.SetSecurityCodes(ctx, codes); err != nil {
			return fmt.Errorf("store codes: %w", err)
		}
		if err := ledger.StorePrices(ctx, queries, domain.PriceSource(feed.Market), domain.Security, prices); err != nil {
			return fmt.Errorf("store quotes: %w", err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("fetch quotes: %w", errors.Join(errs...))
	}
	return nil
}

// row is either list's shape, each with names of its own; prices are strings, kept exactly (ADR 0004).
type row struct {
	Date string `json:"Date"`
	// TWSE.
	Code         string `json:"Code"`
	Name         string `json:"Name"`
	ClosingPrice string `json:"ClosingPrice"`
	// TPEx.
	SecuritiesCompanyCode string `json:"SecuritiesCompanyCode"`
	CompanyName           string `json:"CompanyName"`
	Close                 string `json:"Close"`
}

func (w *Worker) fetch(ctx context.Context, url string) ([]row, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	response, err := w.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("get: %w", err)
	}
	defer func() { _ = response.Body.Close() }() // read-only: nothing to lose if closing fails
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %s", response.Status)
	}
	// TPEx's list, warrants included, is about 5 MB.
	var rows []row
	if err := json.NewDecoder(io.LimitReader(response.Body, 32<<20)).Decode(&rows); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return rows, nil
}

// keep takes the rows of held names that closed at a price, as of the close at 13:30 on the day, and
// their codes; a security that did not trade that day prints none, and its newest close stands.
func keep(market string, rows []row, held map[string]bool) (dbq.SetSecurityCodesParams, []ledger.Price) {
	var codes dbq.SetSecurityCodesParams
	var prices []ledger.Price
	for _, r := range rows {
		code, name, closing := r.Code, r.Name, r.ClosingPrice
		if market == "tpex" {
			code, name, closing = r.SecuritiesCompanyCode, r.CompanyName, r.Close
		}
		// Spelled as statements spell it, so that the close reaches the unit.
		name = domain.Symbol(domain.Security, name)
		day, ok := rocDay(r.Date)
		if !held[name] || !ok {
			continue
		}
		price := strings.TrimSpace(closing)
		if r, ok := new(big.Rat).SetString(price); !ok || r.Sign() <= 0 {
			continue
		}
		codes.Symbols = append(codes.Symbols, name)
		codes.Codes = append(codes.Codes, strings.TrimSpace(code))
		codes.Markets = append(codes.Markets, market)
		prices = append(prices, ledger.Price{
			Symbol: name, AsOf: taipei.At(day.Time, 13, 30), Amount: "1", Quote: domain.BaseCurrency, QuoteAmount: price,
		})
	}
	return codes, prices
}

// rocDay reads the lists' dates, Minguo years without separators: 1151106 is 2026-11-06.
func rocDay(s string) (pgtype.Date, bool) {
	if len(s) != 7 {
		return pgtype.Date{}, false
	}
	year, err := strconv.Atoi(s[:3])
	if err != nil {
		return pgtype.Date{}, false
	}
	day, err := time.Parse("0102", s[3:])
	if err != nil {
		return pgtype.Date{}, false
	}
	return pgtype.Date{Time: time.Date(year+1911, day.Month(), day.Day(), 0, 0, 0, 0, time.UTC), Valid: true}, true
}
