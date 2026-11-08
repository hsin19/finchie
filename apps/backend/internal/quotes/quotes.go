// Package quotes keeps the daily closing prices that give holdings a worth in NT$ (ADR 0017).
package quotes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"golang.org/x/text/unicode/norm"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
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

func (FetchArgs) InsertOpts() river.InsertOpts {
	// The next run is six hours away; a few retries cover an exchange's API that is briefly down.
	return river.InsertOpts{MaxAttempts: 4}
}

// Periodic runs the job every 6 hours and when the backend starts, like the exchange rates.
func Periodic() *river.PeriodicJob {
	return river.NewPeriodicJob(river.PeriodicInterval(6*time.Hour),
		func() (river.JobArgs, *river.InsertOpts) { return FetchArgs{}, nil },
		&river.PeriodicJobOpts{RunOnStart: true, ID: "fetch_quotes"})
}

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
	names, err := queries.HeldSecurities(ctx)
	if err != nil {
		return fmt.Errorf("held securities: %w", err)
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
		params := keep(feed.Market, rows, held)
		if len(params.Names) == 0 {
			continue
		}
		if err := queries.UpsertQuotes(ctx, params); err != nil {
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

// keep takes the rows of held names that closed at a price; a security that did not trade that day
// prints none, and its newest close stands.
func keep(market string, rows []row, held map[string]bool) dbq.UpsertQuotesParams {
	var params dbq.UpsertQuotesParams
	for _, r := range rows {
		code, name, closing := r.Code, r.Name, r.ClosingPrice
		if market == "tpex" {
			code, name, closing = r.SecuritiesCompanyCode, r.CompanyName, r.Close
		}
		// Statements' names are normalized the same way (the parser's clean).
		name = strings.TrimSpace(norm.NFKC.String(name))
		day, ok := rocDay(r.Date)
		if !held[name] || !ok {
			continue
		}
		var price pgtype.Numeric
		if err := price.Scan(strings.TrimSpace(closing)); err != nil || price.Int == nil || price.Int.Sign() <= 0 {
			continue
		}
		params.Days = append(params.Days, day)
		params.Names = append(params.Names, name)
		params.Codes = append(params.Codes, strings.TrimSpace(code))
		params.Markets = append(params.Markets, market)
		params.Closes = append(params.Closes, price)
	}
	return params
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
