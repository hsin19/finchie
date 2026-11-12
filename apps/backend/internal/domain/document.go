package domain

// DocumentStatus is what became of parsing a document (ADR 0005), as stored in documents.status.
type DocumentStatus string

const (
	Pending     DocumentStatus = "pending"
	Parsed      DocumentStatus = "parsed"
	NeedsReview DocumentStatus = "needs_review"
	// Another document already holds the same statement: an e-mail and the same one forwarded.
	Duplicate DocumentStatus = "duplicate"
	Failed    DocumentStatus = "failed"
	// No parser handles the document: nothing to fix or retry.
	Skipped DocumentStatus = "skipped"
)

// DocumentStatuses are every status, in the order the web app lists them.
var DocumentStatuses = []DocumentStatus{Pending, Parsed, NeedsReview, Duplicate, Failed, Skipped}

// Deletable reports whether a document in this status may be deleted: it holds nothing of the ledger (one
// that failed holds no statement, a duplicate's statement is another document's), and no parse is at work
// on it. Deleting one that holds a statement would take the statement, its lines, and what was trimmed or
// linked because of them along; that is not offered.
func (s DocumentStatus) Deletable() bool {
	return s == Failed || s == Skipped || s == Duplicate
}

// Failure is why a document could not be parsed, as stored in documents.failure: what has to change before
// parsing it again can succeed.
type Failure string

const (
	// No parser claims the document (the parser's UNIMPLEMENTED).
	Unsupported Failure = "unsupported"
	// Its PDF did not open: a password the parser needs is missing or wrong (FAILED_PRECONDITION).
	Locked Failure = "locked"
	// It is no e-mail, or cannot be read as one (INVALID_ARGUMENT).
	Unreadable Failure = "unreadable"
	// Its layout is not the one the parser knows (INTERNAL).
	Layout Failure = "layout"
	// The parser could not be reached in time, however often the job tried.
	Unavailable Failure = "unavailable"
	// The parser returned something that breaks the contract.
	InvalidResponse Failure = "invalid_response"
	// Anything else, such as the result failing to store.
	OtherFailure Failure = "other"
)

// Status is the status a document with this failure has: one no parser handles is skipped.
func (f Failure) Status() DocumentStatus {
	if f == Unsupported {
		return Skipped
	}
	return Failed
}

// Outcome is the status of a document whose statement was stored: one that does not add up needs review.
func Outcome(balanced bool) DocumentStatus {
	if balanced {
		return Parsed
	}
	return NeedsReview
}
