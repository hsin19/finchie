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
	"github.com/hsin19/finchie/apps/backend/internal/push"
	"github.com/hsin19/finchie/apps/backend/internal/taipei"
)

// Evaluate works out what needs the owner at now, raises what is new, and resolves what is no longer so.
// What it raised is pushed to the owner's devices, as it happens: what was raised before is never pushed.
// Not while a document is being parsed, though: its ledger is still changing, and a release that parses
// every document again would push gaps that close a minute later. What it raised then is in the app.
func Evaluate(ctx context.Context, pool *pgxpool.Pool, now time.Time) error {
	today := taipei.Today(now)
	var raised []Message
	err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		raised = nil
		queries := dbq.New(tx)
		if err := queries.LockNotifications(ctx); err != nil {
			return fmt.Errorf("lock notifications: %w", err)
		}
		accounts, err := ledger.ReadAccounts(ctx, queries, nil)
		if err != nil {
			return err
		}
		watched := make([]domain.Watched, 0, len(accounts))
		named := make(map[int64]*Account, len(accounts))
		for _, a := range accounts {
			watched = append(watched, a.Watched(today))
			named[a.ID] = &Account{ID: a.ID, Institution: a.Institution, Kind: a.Kind, Label: a.Label, Name: a.Name}
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
		// One key is one situation; should two notices share one, the later stands, as it would one at a
		// time, and a statement may not upsert one row twice.
		last := make(map[string]int, len(notices))
		for i, n := range notices {
			last[n.Key] = i
		}
		// Never nil: an empty list resolves everything open, where NULL would resolve nothing.
		keys := make([]string, 0, len(notices))
		kept := make([]domain.Notice, 0, len(notices))
		facts := make([]Facts, 0, len(notices))
		stored := make([]storedNotice, 0, len(notices))
		for i, n := range notices {
			if last[n.Key] != i {
				continue
			}
			f := factsOf(n, today)
			encoded, err := json.Marshal(f)
			if err != nil {
				return fmt.Errorf("notice facts: %w", err)
			}
			s := storedNotice{Kind: n.Kind, Key: n.Key, Facts: encoded}
			if n.AccountID != 0 {
				s.AccountID = &n.AccountID
			}
			if n.Document != nil {
				s.DocumentID = &n.Document.ID
			}
			keys = append(keys, n.Key)
			kept = append(kept, n)
			facts = append(facts, f)
			stored = append(stored, s)
		}
		batch, err := json.Marshal(stored)
		if err != nil {
			return fmt.Errorf("notices: %w", err)
		}
		changed, err := queries.RaiseNotifications(ctx, batch)
		if err != nil {
			return fmt.Errorf("raise notifications: %w", err)
		}
		news := make(map[string]dbq.RaiseNotificationsRow, len(changed))
		for _, row := range changed {
			if row.Raised {
				news[row.Key] = row
			}
		}
		// In the notices' order, as when they were raised one at a time.
		for i, n := range kept {
			if row, ok := news[n.Key]; ok {
				raised = append(raised, Message{Base: domain.BaseCurrency, Notification: &Pushed{
					ID: row.ID, Kind: n.Kind, RaisedAt: row.RaisedAt.Time, Account: named[n.AccountID], Facts: facts[i],
				}})
			}
		}
		if err := queries.ResolveNotifications(ctx, keys); err != nil {
			return fmt.Errorf("resolve notifications: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("notify: %w", err)
	}
	if len(raised) > 0 {
		pushRaised(ctx, pool, raised)
	}
	return nil
}

// pushRaised pushes what was just raised, unless a document is being parsed. A push that fails is only
// logged: the notification is in the app either way.
func pushRaised(ctx context.Context, pool *pgxpool.Pool, raised []Message) {
	pending, err := dbq.New(pool).AnyDocumentPending(ctx)
	if err != nil {
		slog.WarnContext(ctx, "notifications not pushed", "error", err)
		return
	}
	if pending {
		return
	}
	messages := make([][]byte, 0, len(raised))
	for _, m := range raised {
		encoded, err := json.Marshal(m)
		if err != nil {
			slog.WarnContext(ctx, "notifications not pushed", "error", err)
			return
		}
		messages = append(messages, encoded)
	}
	if _, err := push.Send(ctx, pool, messages...); err != nil {
		slog.WarnContext(ctx, "notifications not all pushed", "error", err)
	}
}

// Message is what a push carries: a notification just raised, as the API shows it, and the base currency
// its amounts are in, for the app's service worker to word it as the app does. Without a notification it
// is a test.
type Message struct {
	Base         string  `json:"base"`
	Notification *Pushed `json:"notification,omitempty"`
}

// Pushed is a notification as the API shows it (api.Notification).
// storedNotice is a notice as RaiseNotifications reads it, one element of its JSON array.
type storedNotice struct {
	Kind       domain.NoticeKind `json:"kind"`
	Key        string            `json:"key"`
	AccountID  *int64            `json:"account_id"`
	DocumentID *int64            `json:"document_id"`
	Facts      json.RawMessage   `json:"facts"`
}

type Pushed struct {
	ID       int64             `json:"id"`
	Kind     domain.NoticeKind `json:"kind"`
	RaisedAt time.Time         `json:"raisedAt"`
	Account  *Account          `json:"account,omitempty"`
	Facts
}

// Account is the account a notification is about, as it is named.
type Account struct {
	ID          int64              `json:"id"`
	Institution string             `json:"institution"`
	Kind        domain.AccountKind `json:"kind"`
	Label       *string            `json:"label,omitempty"`
	Name        *string            `json:"name,omitempty"`
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
