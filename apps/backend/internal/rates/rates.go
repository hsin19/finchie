// Package rates keeps daily reference rates to NT$ for comparing balances in different units (ADR 0015),
// as prices of the money units some account holds or held (ADR 0020).
package rates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/domain"
	"github.com/hsin19/finchie/apps/backend/internal/jobs"
	"github.com/hsin19/finchie/apps/backend/internal/ledger"
	"github.com/hsin19/finchie/apps/backend/internal/numeric"
	"github.com/hsin19/finchie/apps/backend/internal/taipei"
)

// Feeds are tried in order: Fawaz Ahmed's currency API on jsDelivr, then its Cloudflare Pages mirror.
var Feeds = []string{
	"https://cdn.jsdelivr.net/npm/@fawazahmed0/currency-api@latest/v1/currencies/twd.json",
	"https://latest.currency-api.pages.dev/v1/currencies/twd.json",
}

// Source names the feed's prices.
const Source = domain.SourceRates

// FetchArgs is the periodic job that keeps today's rates.
type FetchArgs struct{}

func (FetchArgs) Kind() string { return "fetch_exchange_rates" }

func (FetchArgs) InsertOpts() river.InsertOpts { return jobs.SourceOpts() }

// Periodic reads the feed as a source (jobs.Source).
func Periodic() *river.PeriodicJob { return jobs.Source("fetch_exchange_rates", FetchArgs{}) }

type Worker struct {
	river.WorkerDefaults[FetchArgs]
	pool   *pgxpool.Pool
	client *http.Client
	feeds  []string
}

func NewWorker(pool *pgxpool.Pool, client *http.Client, feeds []string) *Worker {
	return &Worker{pool: pool, client: client, feeds: feeds}
}

func (w *Worker) Timeout(*river.Job[FetchArgs]) time.Duration { return time.Minute }

func (w *Worker) Work(ctx context.Context, _ *river.Job[FetchArgs]) error {
	var errs []error
	for _, url := range w.feeds {
		prices, err := w.fetch(ctx, url)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", url, err))
			continue
		}
		if err := ledger.StorePrices(ctx, dbq.New(w.pool), Source, domain.Money, prices); err != nil {
			return fmt.Errorf("store rates: %w", err)
		}
		return nil
	}
	return fmt.Errorf("fetch rates: %w", errors.Join(errs...))
}

// feed is the API's shape: {"date": "2026-11-07", "twd": {"usd": 0.0305, …}}.
type feed struct {
	Date string                 `json:"date"`
	TWD  map[string]json.Number `json:"twd"`
}

func (w *Worker) fetch(ctx context.Context, url string) ([]ledger.Price, error) {
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
	// A rate is a decimal string kept exactly, never a float (ADR 0004).
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	decoder.UseNumber()
	var body feed
	if err := decoder.Decode(&body); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return parse(body)
}

// parse reads the feed as prices: of each unit, the rate is how much one NT$ buys, as of the start of the
// feed's day in Taipei. The store keeps the units some account holds or held.
func parse(body feed) ([]ledger.Price, error) {
	day, err := time.Parse(time.DateOnly, body.Date)
	if err != nil {
		return nil, fmt.Errorf("date %q: %w", body.Date, err)
	}
	units := make([]string, 0, len(body.TWD))
	for unit := range body.TWD {
		units = append(units, unit)
	}
	sort.Strings(units)
	asOf := taipei.At(day, 0, 0)
	prices := make([]ledger.Price, 0, len(units))
	for _, unit := range units {
		// The feed prints a tiny rate in exponent notation (BTC: 4.0260888e-7), read exactly all the same.
		// A unit priced at zero (or a value that is no number) cannot be divided by: left out.
		r, ok := new(big.Rat).SetString(body.TWD[unit].String())
		if !ok || r.Sign() <= 0 {
			continue
		}
		if _, err := numeric.FromRat(r); err != nil {
			continue
		}
		prices = append(prices, ledger.Price{Symbol: unit, AsOf: asOf, Amount: body.TWD[unit].String(), Quote: domain.BaseCurrency, QuoteAmount: "1"})
	}
	if len(prices) == 0 {
		return nil, errors.New("no rates in the feed")
	}
	return prices, nil
}
