// Package jobs is how background work is scheduled: which queue a job runs on and how often the sources
// that keep arriving by themselves are read. Each job's work is its own package's.
package jobs

import (
	"time"

	"github.com/riverqueue/river"
)

const (
	// Parse runs parse jobs, as many at once as the deployment sets (BACKEND_PARSE_CONCURRENCY).
	Parse = river.QueueDefault
	// Sources runs everything else: reading rates, closes, and exchanges, and queueing stalled documents
	// again. Never held up by documents being parsed, nor holding them up.
	Sources = "sources"
	// SourcesWorkers is how many source jobs run at once.
	SourcesWorkers = 2
)

// SourceInterval is how often a source is read: often enough that a day's prices and balances are there
// the same day, rarely enough to stay well within free APIs' limits.
const SourceInterval = 6 * time.Hour

// Source is a periodic job that reads a source every SourceInterval and when the backend starts. Its id
// keeps one schedule per source across restarts.
func Source(id string, args river.JobArgs) *river.PeriodicJob {
	return river.NewPeriodicJob(river.PeriodicInterval(SourceInterval),
		func() (river.JobArgs, *river.InsertOpts) { return args, nil },
		&river.PeriodicJobOpts{RunOnStart: true, ID: id})
}

// SourceOpts are a source job's: the next run is SourceInterval away, so a few retries cover a source
// that is briefly down, and a run that still fails is simply the next one's to make up.
func SourceOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 4, Queue: Sources}
}
