// Package moderation owns safety reports and the review trail behind them.
//
// A report is a row that never disappears. Its status moves through a small, explicit set
// of transitions, and each move appends an event with the actor and their note, so the
// question "who decided this, when, and why" has an answer that cannot be edited later
// (the database refuses UPDATE and DELETE on the event table).
package moderation

import (
	"errors"
	"time"
)

var (
	// ErrInvalidInput covers a malformed subject, reason or note.
	ErrInvalidInput = errors.New("moderation: invalid input")
	// ErrNotFound means the report, or the thing being reported, does not exist.
	ErrNotFound = errors.New("moderation: not found")
	// ErrForbidden means the caller may not see or act on this report.
	ErrForbidden = errors.New("moderation: not allowed")
	// ErrConflict means the requested transition is not legal from the current state.
	ErrConflict = errors.New("moderation: status transition not allowed")
)

// Subject types a report can point at. Kept deliberately small: a report names one thing.
const (
	SubjectUser    = "user"
	SubjectPost    = "post"
	SubjectMessage = "message"
)

// Reasons are a closed list so the queue can be counted and routed.
const (
	ReasonHarassment    = "harassment"
	ReasonNonConsensual = "nonconsensual"
	ReasonMinor         = "minor"
	ReasonImpersonation = "impersonation"
	ReasonSpam          = "spam"
	ReasonPayment       = "payment"
	ReasonOther         = "other"
)

// Status values.
const (
	StatusOpen      = "open"
	StatusReviewing = "reviewing"
	StatusActioned  = "actioned"
	StatusDismissed = "dismissed"
)

// Actions are what a moderator can do. They are verbs, not statuses, because the same
// target status can be reached from more than one place and the difference matters.
const (
	ActionStartReview = "start_review"
	ActionAction      = "action"
	ActionDismiss     = "dismiss"
	ActionReopen      = "reopen"
)

const (
	MaxDetailsLength = 2000
	MaxNoteLength    = 2000
	MaxQueueSize     = 100
)

// Subjects lists the accepted subject types.
func Subjects() []string { return []string{SubjectUser, SubjectPost, SubjectMessage} }

// Reasons lists the accepted reasons.
func Reasons() []string {
	return []string{
		ReasonHarassment, ReasonNonConsensual, ReasonMinor,
		ReasonImpersonation, ReasonSpam, ReasonPayment, ReasonOther,
	}
}

// Input is a submitted report.
type Input struct {
	SubjectType string `json:"subject_type"`
	SubjectID   string `json:"subject_id"`
	Reason      string `json:"reason"`
	Details     string `json:"details"`
}

// Event is one step in a report's history.
type Event struct {
	ID         int64     `json:"id"`
	Action     string    `json:"action"`
	FromStatus string    `json:"from_status,omitempty"`
	ToStatus   string    `json:"to_status"`
	Note       string    `json:"note,omitempty"`
	ActorID    string    `json:"actor_id,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// Report is a report plus its trail.
type Report struct {
	ID          string     `json:"id"`
	ReporterID  string     `json:"reporter_id"`
	SubjectType string     `json:"subject_type"`
	SubjectID   string     `json:"subject_id"`
	Reason      string     `json:"reason"`
	Details     string     `json:"details,omitempty"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	ResolvedAt  *time.Time `json:"resolved_at,omitempty"`
	ResolvedBy  string     `json:"resolved_by,omitempty"`
	// Events is oldest first. It is empty on list responses and filled on detail.
	Events []Event `json:"events,omitempty"`
	// Duplicate is true when the submission matched an already-open report by the same
	// person on the same subject: the caller learns nothing new happened.
	Duplicate bool `json:"duplicate"`
}

func validSubjectType(v string) bool {
	for _, s := range Subjects() {
		if s == v {
			return true
		}
	}
	return false
}

func validReason(v string) bool {
	for _, s := range Reasons() {
		if s == v {
			return true
		}
	}
	return false
}
