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
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/ingest"
	"github.com/hsin19/finchie/apps/backend/internal/numeric"
	"github.com/hsin19/finchie/apps/backend/internal/taipei"
)

// Feeds are tried in order: Fawaz Ahmed's currency API on jsDelivr, then its Cloudflare Pages mirror.
var Feeds = []string{
	"https://cdn.jsdelivr.net/npm/@fawazahmed0/currency-api@latest/v1/currencies/twd.json",
	"https://latest.currency-api.pages.dev/v1/currencies/twd.json",
}

// Source names the feed's prices.
const Source = "fawaz"

// FetchArgs is the periodic job that keeps today's rates.
type FetchArgs struct{}

func (FetchArgs) Kind() string { return "fetch_exchange_rates" }

func (FetchArgs) InsertOpts() river.InsertOpts {
	// The next run is six hours away; a few retries cover a feed that is briefly down.
	return river.InsertOpts{MaxAttempts: 4}
}

// Periodic runs the job every 6 hours and when the backend starts.
func Periodic() *river.PeriodicJob {
	return river.NewPeriodicJob(river.PeriodicInterval(6*time.Hour),
		func() (river.JobArgs, *river.InsertOpts) { return FetchArgs{}, nil },
		&river.PeriodicJobOpts{RunOnStart: true, ID: "fetch_exchange_rates"})
}

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
		params, err := w.fetch(ctx, url)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", url, err))
			continue
		}
		if err := dbq.New(w.pool).UpsertPrices(ctx, params); err != nil {
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

func (w *Worker) fetch(ctx context.Context, url string) (dbq.UpsertPricesParams, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return dbq.UpsertPricesParams{}, fmt.Errorf("request: %w", err)
	}
	response, err := w.client.Do(request)
	if err != nil {
		return dbq.UpsertPricesParams{}, fmt.Errorf("get: %w", err)
	}
	defer func() { _ = response.Body.Close() }() // read-only: nothing to lose if closing fails
	if response.StatusCode != http.StatusOK {
		return dbq.UpsertPricesParams{}, fmt.Errorf("status %s", response.Status)
	}
	// A rate is a decimal string kept exactly, never a float (ADR 0004).
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	decoder.UseNumber()
	var body feed
	if err := decoder.Decode(&body); err != nil {
		return dbq.UpsertPricesParams{}, fmt.Errorf("decode: %w", err)
	}
	return parse(body)
}

// parse reads the feed as prices: of each unit, the rate is how much one NT$ buys, as of the start of the
// feed's day in Taipei. The store keeps the units some account holds or held.
func parse(body feed) (dbq.UpsertPricesParams, error) {
	day, err := time.Parse(time.DateOnly, body.Date)
	if err != nil {
		return dbq.UpsertPricesParams{}, fmt.Errorf("date %q: %w", body.Date, err)
	}
	units := make([]string, 0, len(body.TWD))
	for unit := range body.TWD {
		units = append(units, unit)
	}
	sort.Strings(units)
	asOf := pgtype.Timestamptz{Time: taipei.At(day, 0, 0), Valid: true}
	one := pgtype.Numeric{Int: big.NewInt(1), Valid: true}
	params := dbq.UpsertPricesParams{Source: Source, Kind: ingest.UnitMoney}
	for _, unit := range units {
		// The feed prints a tiny rate in exponent notation (BTC: 4.0260888e-7), read exactly all the same.
		// A unit priced at zero (or a value that is no number) cannot be divided by: left out.
		r, ok := new(big.Rat).SetString(body.TWD[unit].String())
		if !ok || r.Sign() <= 0 {
			continue
		}
		rate, err := numeric.FromRat(r)
		if err != nil {
			continue
		}
		params.Symbols = append(params.Symbols, strings.ToUpper(unit))
		params.AsOf = append(params.AsOf, asOf)
		params.Amounts = append(params.Amounts, rate)
		params.Quotes = append(params.Quotes, "TWD")
		params.QuoteAmounts = append(params.QuoteAmounts, one)
	}
	if len(params.Symbols) == 0 {
		return dbq.UpsertPricesParams{}, errors.New("no rates in the feed")
	}
	return params, nil
}
