// Package notify tells the owner what needs them (domain.Notices), once: it raises each situation as a
// notification, keeps what the owner did with it, and resolves it by itself once it is no longer so, so
// that nothing is left for the owner to clear (AGENTS.md: no work for the owner). It is worked out again
// from the ledger whenever that may have changed: after a document is stored, after the owner changes
// something, and every hour, since a day passing can make an account overdue or a bill due.
package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/domain"
	"github.com/hsin19/finchie/apps/backend/internal/jobs"
	"github.com/hsin19/finchie/apps/backend/internal/ledger"
	"github.com/hsin19/finchie/apps/backend/internal/numeric"
	"github.com/hsin19/finchie/apps/backend/internal/taipei"
)

// Evaluate works out what needs the owner at now, raises what is new, and resolves what is no longer so.
func Evaluate(ctx context.Context, pool *pgxpool.Pool, now time.Time) error {
	today := taipei.Today(now)
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		queries := dbq.New(tx)
		if err := queries.LockNotifications(ctx); err != nil {
			return fmt.Errorf("lock notifications: %w", err)
		}
		accounts, err := ledger.ReadAccounts(ctx, queries, nil)
		if err != nil {
			return err
		}
		watched := make([]domain.Watched, 0, len(accounts))
		for _, a := range accounts {
			watched = append(watched, a.Watched(today))
		}
		rows, err := queries.TroubledDocuments(ctx)
		if err != nil {
			return fmt.Errorf("troubled documents: %w", err)
		}
		documents := make([]domain.TroubledDocument, 0, len(rows))
		for _, d := range rows {
			documents = append(documents, domain.TroubledDocument{ID: d.ID, Filename: d.Filename, Status: d.Status, Failure: d.Failure})
		}

		notices := domain.Notices(watched, documents, today)
		// Never nil: an empty list resolves everything open, where NULL would resolve nothing.
		keys := make([]string, 0, len(notices))
		for _, n := range notices {
			facts, err := json.Marshal(factsOf(n, today))
			if err != nil {
				return fmt.Errorf("notice facts: %w", err)
			}
			params := dbq.RaiseNotificationParams{Kind: n.Kind, Key: n.Key, Facts: facts}
			if n.AccountID != 0 {
				params.AccountID = &n.AccountID
			}
			if n.Document != nil {
				params.DocumentID = &n.Document.ID
			}
			if err := queries.RaiseNotification(ctx, params); err != nil {
				return fmt.Errorf("raise %s: %w", n.Key, err)
			}
			keys = append(keys, n.Key)
		}
		if err := queries.ResolveNotifications(ctx, keys); err != nil {
			return fmt.Errorf("resolve notifications: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("notify: %w", err)
	}
	return nil
}

// After evaluates once the work that may have changed the ledger is done. A failure is only logged: what
// it missed, the next evaluation, within the hour, makes up for.
func After(ctx context.Context, pool *pgxpool.Pool) {
	if err := Evaluate(context.WithoutCancel(ctx), pool, time.Now()); err != nil {
		slog.WarnContext(ctx, "notifications not worked out", "error", err)
	}
}

// Facts are what was so when a situation was raised, stored as the API shows it (api.Notification).
type Facts struct {
	Document  *Document  `json:"document,omitempty"`
	Shortfall *Shortfall `json:"shortfall,omitempty"`
	Gap       *Gap       `json:"gap,omitempty"`
	Newest    string     `json:"newest,omitempty"`
	Lapsing   *Lapsing   `json:"lapsing,omitempty"`
}

type Document struct {
	Filename string  `json:"filename"`
	Status   string  `json:"status"`
	Failure  *string `json:"failure,omitempty"`
}

type Debit struct {
	StatementID int64  `json:"statementId"`
	Institution string `json:"institution"`
	DueDate     string `json:"dueDate"`
	Unit        string `json:"unit"`
	Amount      string `json:"amount"`
	PaidOn      string `json:"paidOn,omitempty"`
}

type Shortfall struct {
	Unit    string  `json:"unit"`
	AsOf    string  `json:"asOf"`
	Balance string  `json:"balance"`
	Debits  []Debit `json:"debits"`
	Owed    string  `json:"owed"`
	Lacks   string  `json:"lacks"`
}

type Gap struct {
	Unit   string `json:"unit"`
	From   string `json:"from"`
	To     string `json:"to"`
	Unseen string `json:"unseen"`
}

type Lapsing struct {
	Unit      string `json:"unit"`
	Amount    string `json:"amount"`
	By        string `json:"by"`
	Estimated bool   `json:"estimated"`
	Soon      bool   `json:"soon"`
}

func factsOf(n domain.Notice, today time.Time) Facts {
	var facts Facts
	switch {
	case n.Document != nil:
		d := Document{Filename: n.Document.Filename, Status: string(n.Document.Status)}
		if n.Document.Failure != nil {
			failure := string(*n.Document.Failure)
			d.Failure = &failure
		}
		facts.Document = &d
	case n.Shortfall != nil:
		s := n.Shortfall
		shortfall := Shortfall{
			Unit: s.Unit, AsOf: day(s.AsOf), Balance: numeric.Exact(s.Balance), Owed: numeric.Exact(s.Owed),
			Lacks: numeric.Exact(s.Lacks), Debits: make([]Debit, 0, len(s.Debits)),
		}
		for _, b := range s.Debits {
			shortfall.Debits = append(shortfall.Debits, DebitOf(b))
		}
		facts.Shortfall = &shortfall
	case n.Gap != nil:
		facts.Gap = &Gap{Unit: n.Gap.Unit, From: day(n.Gap.From), To: day(n.Gap.To), Unseen: numeric.Exact(n.Gap.Unseen)}
	case n.Lapsing != nil:
		l := LapsingOf(*n.Lapsing, today)
		facts.Lapsing = &l
	default:
		facts.Newest = day(n.Newest)
	}
	return facts
}

// DebitOf is a card bill as an account's autopay takes it.
func DebitOf(b domain.CardBill) Debit {
	d := Debit{StatementID: b.ID, Institution: b.Institution, DueDate: day(b.DueDate), Unit: b.Unit, Amount: numeric.Exact(b.TotalDue)}
	if !b.PaidOn.IsZero() {
		d.PaidOn = day(b.PaidOn)
	}
	return d
}

// LapsingOf is points to lapse, as shown on today.
func LapsingOf(l domain.Lapsing, today time.Time) Lapsing {
	return Lapsing{Unit: l.Unit, Amount: numeric.Exact(l.Amount), By: day(l.By), Estimated: l.Estimated, Soon: l.Soon(today)}
}

func day(t time.Time) string { return t.Format(time.DateOnly) }

// Args is the job that works notifications out every hour and when the backend starts, so that a day
// passing counts.
type Args struct{}

func (Args) Kind() string { return "notify" }

func (Args) InsertOpts() river.InsertOpts {
	// The next run is an hour away: a failed one is that one's to make up.
	return river.InsertOpts{MaxAttempts: 1, Queue: jobs.Sources}
}

// Periodic schedules it.
func Periodic() *river.PeriodicJob {
	return river.NewPeriodicJob(river.PeriodicInterval(time.Hour),
		func() (river.JobArgs, *river.InsertOpts) { return Args{}, nil },
		&river.PeriodicJobOpts{RunOnStart: true, ID: "notify"})
}

type Worker struct {
	river.WorkerDefaults[Args]
	pool *pgxpool.Pool
}

func NewWorker(pool *pgxpool.Pool) *Worker { return &Worker{pool: pool} }

func (w *Worker) Timeout(*river.Job[Args]) time.Duration { return time.Minute }

func (w *Worker) Work(ctx context.Context, _ *river.Job[Args]) error {
	return Evaluate(ctx, w.pool, time.Now())
}
