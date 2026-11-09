// Package exchanges keeps snapshots of what the owner holds at crypto exchanges, read from their APIs
// with read-only keys (ADR 0018). Each exchange is an adapter that ends in a Snapshot; storing one knows
// nothing of which exchange reported it.
package exchanges

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
)

// Snapshot is what an exchange account holds now, and what the exchange says the whole is worth.
type Snapshot struct {
	// By unit: the coin's code, as the exchange prints it. Wallets that report only their value in USDT
	// (futures, bots) are added to USDT.
	Holdings map[string]*big.Rat
	// Nil when the exchange gives none, or cannot price everything held.
	Valuation *Value
}

type Value struct {
	Unit   string
	Amount *big.Rat
}

// add adds amount to the holding of unit; a coin is held in several wallets.
func (s *Snapshot) add(unit string, amount *big.Rat) {
	if s.Holdings == nil {
		s.Holdings = map[string]*big.Rat{}
	}
	unit = strings.ToUpper(unit)
	if held, ok := s.Holdings[unit]; ok {
		held.Add(held, amount)
		return
	}
	s.Holdings[unit] = new(big.Rat).Set(amount)
}

// Exchange reads one exchange's snapshot. Every call it makes succeeds, or it returns an error: a snapshot
// missing a wallet would read as that wallet's coins sold.
type Exchange interface {
	Institution() string
	Fetch(ctx context.Context) (Snapshot, error)
}

// FetchArgs is the periodic job that keeps one exchange's snapshot, one job per exchange so that each
// retries on its own.
type FetchArgs struct {
	Institution string `json:"institution"`
}

func (FetchArgs) Kind() string { return "fetch_exchange" }

func (FetchArgs) InsertOpts() river.InsertOpts {
	// The next run is six hours away; a few retries cover an API that is briefly down.
	return river.InsertOpts{MaxAttempts: 4}
}

// Periodic runs each exchange's job every 6 hours and when the backend starts, like the rates and quotes.
func Periodic(exchanges []Exchange) []*river.PeriodicJob {
	jobs := make([]*river.PeriodicJob, 0, len(exchanges))
	for _, e := range exchanges {
		institution := e.Institution()
		jobs = append(jobs, river.NewPeriodicJob(river.PeriodicInterval(6*time.Hour),
			func() (river.JobArgs, *river.InsertOpts) { return FetchArgs{Institution: institution}, nil },
			&river.PeriodicJobOpts{RunOnStart: true, ID: "fetch_exchange_" + institution}))
	}
	return jobs
}

type Worker struct {
	river.WorkerDefaults[FetchArgs]
	pool      *pgxpool.Pool
	exchanges map[string]Exchange
	now       func() time.Time
}

func NewWorker(pool *pgxpool.Pool, exchanges []Exchange, now func() time.Time) *Worker {
	byInstitution := make(map[string]Exchange, len(exchanges))
	for _, e := range exchanges {
		byInstitution[e.Institution()] = e
	}
	return &Worker{pool: pool, exchanges: byInstitution, now: now}
}

func (w *Worker) Timeout(*river.Job[FetchArgs]) time.Duration { return 2 * time.Minute }

func (w *Worker) Work(ctx context.Context, job *river.Job[FetchArgs]) error {
	exchange, ok := w.exchanges[job.Args.Institution]
	if !ok {
		// Its keys were removed after the job was queued.
		slog.InfoContext(ctx, "exchange not configured, skipping", "institution", job.Args.Institution)
		return nil
	}
	snapshot, err := exchange.Fetch(ctx)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", job.Args.Institution, err)
	}
	return Store(ctx, w.pool, job.Args.Institution, Today(w.now()), snapshot)
}

// taipei is the owner's day (ADR 0004). Taiwan has kept UTC+8 without daylight saving since 1979, and a
// fixed zone needs no time zone database in the image.
var taipei = time.FixedZone("Asia/Taipei", 8*60*60)

// Today is the date in Taipei at t, as a date without time zone.
func Today(t time.Time) time.Time {
	y, m, d := t.In(taipei).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// decimal is an amount as an API prints it, a JSON string or number, kept as text until read exactly;
// null is empty.
type decimal string

func (d *decimal) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*d = ""
		return nil
	}
	if len(b) > 0 && b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return fmt.Errorf("amount %s: %w", b, err)
		}
		*d = decimal(s)
		return nil
	}
	*d = decimal(b)
	return nil
}

// quantity reads an amount the API prints as a decimal string; empty or absent is none.
func quantity(s string) (*big.Rat, error) {
	if s == "" {
		return new(big.Rat), nil
	}
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return nil, fmt.Errorf("amount %q is not a number", s)
	}
	return r, nil
}

// sum adds up decimal strings exactly.
func sum(values ...string) (*big.Rat, error) {
	total := new(big.Rat)
	for _, v := range values {
		r, err := quantity(v)
		if err != nil {
			return nil, err
		}
		total.Add(total, r)
	}
	return total, nil
}

// get sends a request and decodes a JSON reply; a reply that is not 200 becomes an error with what the
// exchange said, never the request's headers, which carry the key.
func get(client *http.Client, request *http.Request, into any) error {
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("%s %s: %w", request.Method, request.URL.Path, err)
	}
	defer func() { _ = response.Body.Close() }() // read-only: nothing to lose if closing fails
	body, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("%s %s: read: %w", request.Method, request.URL.Path, err)
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%s %s: %s: %s", request.Method, request.URL.Path, response.Status, detail(body))
	}
	// Amounts stay strings until read exactly (ADR 0004).
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	if err := decoder.Decode(into); err != nil {
		return fmt.Errorf("%s %s: decode: %w", request.Method, request.URL.Path, err)
	}
	return nil
}

var title = regexp.MustCompile(`(?is)<title>([^<]*)</title>`)

// detail is what an error reply says: its JSON as sent, or an edge's block page by its title (Binance's
// CloudFront, MAX's WAF), else the start of the body.
func detail(body []byte) string {
	text := strings.Join(strings.Fields(string(body)), " ")
	if json.Valid(body) {
		return truncate(text, 300)
	}
	if m := title.FindStringSubmatch(text); m != nil {
		return strings.TrimSpace(m[1])
	}
	return truncate(text, 200)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
