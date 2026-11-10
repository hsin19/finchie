package ingest

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	parserv2 "github.com/hsin19/finchie/apps/backend/internal/gen/finchie/parser/v2"
	"github.com/hsin19/finchie/apps/backend/internal/jobs"
)

// A document's parse job can be lost, and a parser out of reach for longer than a job retries fails it.
// Neither says anything about the document. And what a parser made of a document can change: a new
// version reads it differently, a new parser or a password set since can read one that failed. None of
// it should wait for the owner to notice: Retry queues all of them again, every hour and when the backend
// starts (ADR 0005). A document whose job is still queued or running keeps that one (ParseDocumentArgs is
// unique).

// RetryArgs is the periodic job that queues stalled documents again.
type RetryArgs struct{}

func (RetryArgs) Kind() string { return "retry_documents" }

func (RetryArgs) InsertOpts() river.InsertOpts {
	// The next run is an hour away.
	return river.InsertOpts{MaxAttempts: 1, Queue: jobs.Sources}
}

// RetryPeriodic runs Retry every hour and when the backend starts.
func RetryPeriodic() *river.PeriodicJob {
	return river.NewPeriodicJob(river.PeriodicInterval(time.Hour),
		func() (river.JobArgs, *river.InsertOpts) { return RetryArgs{}, nil },
		&river.PeriodicJobOpts{RunOnStart: true, ID: "retry_documents"})
}

type RetryWorker struct {
	river.WorkerDefaults[RetryArgs]
	pool   *pgxpool.Pool
	parser parserv2.ParserServiceClient
}

func NewRetryWorker(pool *pgxpool.Pool, parser parserv2.ParserServiceClient) *RetryWorker {
	return &RetryWorker{pool: pool, parser: parser}
}

func (w *RetryWorker) Timeout(*river.Job[RetryArgs]) time.Duration { return time.Minute }

func (w *RetryWorker) Work(ctx context.Context, _ *river.Job[RetryArgs]) error {
	client, err := river.ClientFromContextSafely[pgx.Tx](ctx)
	if err != nil {
		return fmt.Errorf("river client: %w", err)
	}
	// Without the parser, which documents are outdated is not known; stalled ones are queued all the same.
	described, describeErr := w.parser.DescribeParsers(ctx, &parserv2.DescribeParsersRequest{})
	if describeErr != nil {
		slog.WarnContext(ctx, "parser not described; outdated documents wait for the next run", "error", describeErr)
	}
	var queued int
	err = pgx.BeginFunc(ctx, w.pool, func(tx pgx.Tx) error {
		queries := dbq.New(tx)
		ids, err := queries.StalledDocuments(ctx)
		if err != nil {
			return fmt.Errorf("stalled documents: %w", err)
		}
		if describeErr == nil {
			current := make([]string, 0, len(described.GetParsers()))
			for _, p := range described.GetParsers() {
				current = append(current, p.GetName()+"@"+p.GetVersion())
			}
			outdated, err := queries.OutdatedDocuments(ctx, dbq.OutdatedDocumentsParams{Current: current, Instance: described.GetInstance()})
			if err != nil {
				return fmt.Errorf("outdated documents: %w", err)
			}
			ids = append(ids, outdated...)
		}
		for _, id := range ids {
			if err := queries.MarkDocumentPending(ctx, id); err != nil {
				return fmt.Errorf("mark document %d pending: %w", id, err)
			}
			result, err := client.InsertTx(ctx, tx, ParseDocumentArgs{DocumentID: id}, nil)
			if err != nil {
				return fmt.Errorf("queue document %d: %w", id, err)
			}
			if !result.UniqueSkippedAsDuplicate {
				queued++
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("retry stalled documents: %w", err)
	}
	if queued > 0 {
		slog.InfoContext(ctx, "documents queued again", "documents", queued)
	}
	return nil
}
