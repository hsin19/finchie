package domain

import (
	"slices"
	"strings"
	"time"
)

// StatementKey is what makes two statements the same one (ADR 0013), whichever document carries it: an
// e-mail and the same one forwarded hash differently but report the same accounts over the same period.
type StatementKey struct {
	Institution string
	Kind        AccountKind
	// AccountNumbers of the accounts it reports.
	Accounts string
	// Nil for a card bill, which closes without a period of its own.
	PeriodStart *time.Time
	Closing     time.Time
}

// AccountNumbers keys the accounts a statement reports by their numbers: sorted, each once, joined by
// commas. A bank's card and points accounts have no number, being its only ones of their kind.
func AccountNumbers(numbers ...string) string {
	kept := slices.DeleteFunc(slices.Clone(numbers), func(n string) bool { return n == "" })
	slices.Sort(kept)
	return strings.Join(slices.Compact(kept), ",")
}
