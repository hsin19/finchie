// Package ingest parses stored documents in background jobs and stores the results (ADR 0005).
package ingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/hsin19/finchie/apps/backend/internal/db/dbq"
	"github.com/hsin19/finchie/apps/backend/internal/domain"
	parserv2 "github.com/hsin19/finchie/apps/backend/internal/gen/finchie/parser/v2"
	"github.com/hsin19/finchie/apps/backend/internal/ledger"
)

// ParseDocumentArgs is the job enqueued on upload, on re-parse, and by Retry.
type ParseDocumentArgs struct {
	DocumentID int64 `json:"document_id"`
}

func (ParseDocumentArgs) Kind() string { return "parse_document" }

func (ParseDocumentArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		// Retries only happen while the parser is unreachable; a few minutes of back-off covers a restart,
		// and Retry takes over after that.
		MaxAttempts: 6,
		// A document has one job at a time: a second would race the first to record the outcome. One that
		// has finished does not count, so that a document can be parsed again.
		UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: []rivertype.JobState{
			rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRetryable,
			rivertype.JobStateRunning, rivertype.JobStateScheduled,
		}},
	}
}

type Worker struct {
	river.WorkerDefaults[ParseDocumentArgs]
	pool    *pgxpool.Pool
	parser  parserv2.ParserServiceClient
	timeout time.Duration
}

func NewWorker(pool *pgxpool.Pool, parser parserv2.ParserServiceClient, timeout time.Duration) *Worker {
	return &Worker{pool: pool, parser: parser, timeout: timeout}
}

// Timeout leaves room to store the result after the parser's own deadline.
func (w *Worker) Timeout(*river.Job[ParseDocumentArgs]) time.Duration {
	return w.timeout + 30*time.Second
}

func (w *Worker) Work(ctx context.Context, job *river.Job[ParseDocumentArgs]) error {
	id := job.Args.DocumentID
	queries := dbq.New(w.pool)
	doc, err := queries.GetDocumentContent(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return river.JobCancel(fmt.Errorf("document %d no longer exists", id))
	}
	if err != nil {
		return w.retry(ctx, job, "", domain.OtherFailure, fmt.Errorf("load document %d: %w", id, err))
	}

	callCtx, cancel := context.WithTimeout(ctx, w.timeout)
	// Which instance parses it, so that a failure can be tried again under a later one (Retry).
	described, err := w.parser.DescribeParsers(callCtx, &parserv2.DescribeParsersRequest{})
	if err != nil {
		cancel()
		return w.retry(ctx, job, "", domain.Unavailable, fmt.Errorf("describe parsers: %w", err))
	}
	instance := described.GetInstance()
	response, err := w.parser.ParseDocument(callCtx, &parserv2.ParseDocumentRequest{
		DocumentId: strconv.FormatInt(id, 10),
		Content:    doc.Content,
	})
	cancel()

	if err != nil {
		failure, message := classify(err)
		// A job cancelled because the backend is stopping says nothing about the document.
		if failure == domain.Unavailable || ctx.Err() != nil {
			return w.retry(ctx, job, instance, domain.Unavailable, fmt.Errorf("parse document %d: %w", id, err))
		}
		return w.fail(ctx, id, instance, failure, message)
	}

	outcome, err := Store(ctx, w.pool, id, instance, response)
	if errors.Is(err, ErrInvalidResponse) {
		return w.fail(ctx, id, instance, domain.InvalidResponse, err.Error())
	}
	if err != nil {
		return w.retry(ctx, job, instance, domain.OtherFailure, fmt.Errorf("store document %d: %w", id, err))
	}
	slog.InfoContext(ctx, "document parsed", "document_id", id, "status", outcome,
		"parser", response.GetParser(), "accounts", len(response.GetStatement().GetAccounts()))
	return nil
}

// retry returns err for River to run the job again. After the last attempt nothing would, and the
// document would stay pending for good, so it is marked failed instead, for Retry to take up again when
// the failure is the parser being out of reach.
func (w *Worker) retry(ctx context.Context, job *river.Job[ParseDocumentArgs], instance string, failure domain.Failure, err error) error {
	if job.Attempt < job.MaxAttempts {
		return err
	}
	return w.fail(ctx, job.Args.DocumentID, instance, failure, err.Error())
}

// fail records why a document cannot be parsed and stops River from retrying it. A failed document holds
// no statement: what an earlier parse of it derived is deleted with it, so nothing a parser can no longer
// read stays counted. The write outlives the job's context: a failure found as the job is cancelled (on
// shutdown, or at its timeout) must still be recorded.
func (w *Worker) fail(ctx context.Context, id int64, instance string, failure domain.Failure, detail string) error {
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	err := pgx.BeginFunc(writeCtx, w.pool, func(tx pgx.Tx) error {
		queries := dbq.New(tx)
		if err := queries.LockLinks(writeCtx); err != nil {
			return fmt.Errorf("lock links: %w", err)
		}
		deleted, err := queries.DeleteStatementsForDocument(writeCtx, id)
		if err != nil {
			return fmt.Errorf("delete statements: %w", err)
		}
		if len(deleted) > 0 {
			if _, err := ledger.Relink(writeCtx, queries); err != nil {
				return err
			}
		}
		return queries.FinishDocument(writeCtx, dbq.FinishDocumentParams{
			ID:             id,
			Status:         failure.Status(),
			Failure:        &failure,
			Error:          &detail,
			ParserInstance: optionalString(instance),
			Warnings:       []string{},
		})
	})
	if err != nil {
		return fmt.Errorf("mark document %d failed: %w", id, err)
	}
	// The details stay in the database, beside the statement they come from: a parser's words can quote
	// it (an account number, a balance), and logs and job errors leave the process (ADR 0011).
	slog.WarnContext(ctx, "document failed", "document_id", id, "failure", failure)
	return river.JobCancel(fmt.Errorf("document %d: %s", id, failure))
}

// classify reads the parser's status (ADR 0005): what has to change before the document can be parsed,
// and the parser's words for it. Unavailable is the only failure that may pass by itself.
func classify(err error) (domain.Failure, string) {
	s := status.Convert(err)
	switch s.Code() {
	case codes.Unimplemented:
		return domain.Unsupported, s.Message()
	case codes.FailedPrecondition:
		return domain.Locked, s.Message()
	case codes.InvalidArgument:
		return domain.Unreadable, s.Message()
	case codes.Internal:
		return domain.Layout, s.Message()
	case codes.Unavailable, codes.DeadlineExceeded, codes.ResourceExhausted, codes.Canceled:
		return domain.Unavailable, s.Message()
	default:
		return domain.OtherFailure, fmt.Sprintf("%s: %s", s.Code(), s.Message())
	}
}
