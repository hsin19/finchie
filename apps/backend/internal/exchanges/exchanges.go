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

	"github.com/hsin19/finchie/apps/backend/internal/domain"
	"github.com/hsin19/finchie/apps/backend/internal/jobs"
)

// Snapshot is what an exchange account holds now, and what the exchange says the whole is worth.
type Snapshot struct {
	// By unit: the coin's code, as the exchange prints it. Wallets that report only their value in USDT
	// (futures, bots) are added to USDT.
	Holdings map[string]*big.Rat
	// Nil when the exchange gives none, or cannot price everything held.
	Valuation *Value
	// What one of each coin held is worth at the exchange's own markets, in NT$ or USDT as it prices
	// them (ADR 0020).
	Prices map[string]Value
}

// Movement is money or coins the owner moved into the exchange or out of it, as the exchange records it
// (ADR 0018): fiat from or to a bank, which links to the bank's line, or coins on a chain, which links to
// the other exchange's by the chain's transaction (ADR 0016). Trades, conversions, and earn are not read.
type Movement struct {
	// The exchange's own id for it, which every later read reports again.
	ID string
	At time.Time
	// A currency's or a coin's code, as the exchange prints it.
	Unit string
	// What crossed between the exchange and the other end, its fee apart: positive in, negative out.
	Amount *big.Rat
	// What the exchange took for it, in FeeUnit; nil when nothing.
	Fee     *big.Rat
	FeeUnit string
	// The chain's transaction, which the other end records too; empty for fiat.
	TxID string
	// How fiat came or went, as the exchange tells: a virtual account, a method.
	Memo string
	// Not settled yet: it may still complete or fail, and is not kept until it completes.
	Pending bool
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
	unit = domain.Symbol(domain.Money, unit)
	if held, ok := s.Holdings[unit]; ok {
		held.Add(held, amount)
		return
	}
	s.Holdings[unit] = new(big.Rat).Set(amount)
}

// price keeps what one of unit is worth in quote; a unit is not priced in itself.
func (s *Snapshot) price(unit, quote string, amount *big.Rat) {
	unit, quote = domain.Symbol(domain.Money, unit), domain.Symbol(domain.Money, quote)
	if unit == quote {
		return
	}
	if s.Prices == nil {
		s.Prices = map[string]Value{}
	}
	s.Prices[unit] = Value{Unit: quote, Amount: amount}
}

// Exchange reads one exchange: its snapshot, and the deposits and withdrawals created in a span. Every
// call it makes succeeds, or it returns an error: a snapshot missing a wallet would read as that wallet's
// coins sold, and a span missing a kind of movement would never be read again.
type Exchange interface {
	Institution() string
	Fetch(ctx context.Context) (Snapshot, error)
	// Those created from `from` on, before `to`, settled or not; failed and canceled ones left out.
	Movements(ctx context.Context, from, to time.Time) ([]Movement, error)
}

// HistorySpan is how much of an exchange's deposits and withdrawals one fetch reads: a month on from where
// the last left off, so two years are caught up with in a few days of fetches, and then what came since
// the last one. The exchanges allow up to 90 days.
const HistorySpan = 30 * 24 * time.Hour

// HistoryStart is how far back an exchange's deposits and withdrawals are read the first time.
const HistoryStart = 2 // years

// History is what one fetch read of an exchange's deposits and withdrawals: the span, and what was in it.
type History struct {
	From, To  time.Time
	Movements []Movement
}

// next is where the next read starts: past this span, but not past a movement still pending, which may yet
// complete; one pending longer than a span is given up on, so the reads move on.
func (h History) next(at time.Time) time.Time {
	next := h.To
	for _, m := range h.Movements {
		if m.Pending && at.Sub(m.At) < HistorySpan && m.At.Before(next) && !m.At.Before(h.From) {
			next = m.At
		}
	}
	return next
}

// FetchArgs is the periodic job that keeps one exchange's snapshot, one job per exchange so that each
// retries on its own.
type FetchArgs struct {
	Institution string `json:"institution"`
}

func (FetchArgs) Kind() string { return "fetch_exchange" }

func (FetchArgs) InsertOpts() river.InsertOpts { return jobs.SourceOpts() }

// Periodic reads each exchange as a source of its own (jobs.Source).
func Periodic(exchanges []Exchange) []*river.PeriodicJob {
	periodic := make([]*river.PeriodicJob, 0, len(exchanges))
	for _, e := range exchanges {
		periodic = append(periodic, jobs.Source("fetch_exchange_"+e.Institution(), FetchArgs{Institution: e.Institution()}))
	}
	return periodic
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
	at := w.now()
	from, err := readFrom(ctx, w.pool, job.Args.Institution, at)
	if err != nil {
		return err
	}
	to := from.Add(HistorySpan)
	if to.After(at) {
		to = at
	}
	moves, err := exchange.Movements(ctx, from, to)
	if err != nil {
		return fmt.Errorf("fetch %s deposits and withdrawals: %w", job.Args.Institution, err)
	}
	return Store(ctx, w.pool, job.Args.Institution, at, snapshot, History{From: from, To: to, Movements: moves})
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
