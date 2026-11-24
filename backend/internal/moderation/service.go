package moderation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jawuu187-ops/content-platform/backend/internal/identity"
)

// Service reads and writes reports.
type Service struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
}

// Option configures the service.
type Option func(*Service)

// WithLogger sets the logger used for audit lines.
func WithLogger(l *slog.Logger) Option {
	return func(s *Service) {
		if l != nil {
			s.logger = l
		}
	}
}

// New builds the service on an existing pool.
func New(pool *pgxpool.Pool, opts ...Option) *Service {
	s := &Service{pool: pool, logger: slog.Default()}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Submit records a report. The subject must exist and the reporter must be able to see it:
// a private message can only be reported by someone in that conversation, so this endpoint
// cannot be used to probe whether an arbitrary id exists.
func (s *Service) Submit(ctx context.Context, p identity.Principal, in Input) (Report, error) {
	in.SubjectType = strings.TrimSpace(in.SubjectType)
	in.SubjectID = strings.TrimSpace(in.SubjectID)
	in.Reason = strings.TrimSpace(in.Reason)
	in.Details = strings.TrimSpace(in.Details)

	if !validSubjectType(in.SubjectType) {
		return Report{}, fmt.Errorf("%w: subject_type must be one of %s", ErrInvalidInput,
			strings.Join(Subjects(), ", "))
	}
	if !validReason(in.Reason) {
		return Report{}, fmt.Errorf("%w: reason must be one of %s", ErrInvalidInput,
			strings.Join(Reasons(), ", "))
	}
	if in.SubjectID == "" {
		return Report{}, fmt.Errorf("%w: subject_id is required", ErrInvalidInput)
	}
	if len([]rune(in.Details)) > MaxDetailsLength {
		return Report{}, fmt.Errorf("%w: details are limited to %d characters", ErrInvalidInput, MaxDetailsLength)
	}
	if in.SubjectType == SubjectUser && in.SubjectID == p.UserID {
		return Report{}, fmt.Errorf("%w: you cannot report yourself", ErrInvalidInput)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("moderation: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	visible, err := s.subjectVisible(ctx, tx, p.UserID, in.SubjectType, in.SubjectID)
	if err != nil {
		return Report{}, err
	}
	if !visible {
		return Report{}, ErrNotFound
	}

	// An open report from the same person on the same subject is the same complaint.
	var existing Report
	err = tx.QueryRow(ctx, `
		SELECT id::text, reporter_id::text, subject_type, subject_id::text, reason, details,
		       status, created_at, updated_at
		  FROM moderation.reports
		 WHERE reporter_id = $1::uuid AND subject_type = $2 AND subject_id = $3::uuid
		   AND status IN ('open', 'reviewing')`,
		p.UserID, in.SubjectType, in.SubjectID).Scan(
		&existing.ID, &existing.ReporterID, &existing.SubjectType, &existing.SubjectID,
		&existing.Reason, &existing.Details, &existing.Status,
		&existing.CreatedAt, &existing.UpdatedAt)
	switch {
	case err == nil:
		existing.Duplicate = true
		return existing, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return Report{}, fmt.Errorf("moderation: check duplicate: %w", err)
	}

	var rep Report
	err = tx.QueryRow(ctx, `
		INSERT INTO moderation.reports (reporter_id, subject_type, subject_id, reason, details)
		VALUES ($1::uuid, $2, $3::uuid, $4, $5)
		RETURNING id::text, reporter_id::text, subject_type, subject_id::text, reason, details,
		          status, created_at, updated_at`,
		p.UserID, in.SubjectType, in.SubjectID, in.Reason, in.Details).Scan(
		&rep.ID, &rep.ReporterID, &rep.SubjectType, &rep.SubjectID,
		&rep.Reason, &rep.Details, &rep.Status, &rep.CreatedAt, &rep.UpdatedAt)
	if err != nil {
		return Report{}, fmt.Errorf("moderation: insert report: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO moderation.report_events (report_id, actor_id, from_status, to_status, note)
		VALUES ($1::uuid, $2::uuid, NULL, 'open', '')`, rep.ID, p.UserID); err != nil {
		return Report{}, fmt.Errorf("moderation: insert event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Report{}, fmt.Errorf("moderation: commit: %w", err)
	}
	s.logger.InfoContext(ctx, "report submitted",
		slog.String("report_id", rep.ID),
		slog.String("subject_type", rep.SubjectType),
		slog.String("reason", rep.Reason))
	return rep, nil
}

// Mine returns the caller's own reports, newest first.
func (s *Service) Mine(ctx context.Context, p identity.Principal) ([]Report, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, reporter_id::text, subject_type, subject_id::text, reason, details,
		       status, created_at, updated_at, resolved_at, COALESCE(resolved_by::text, '')
		  FROM moderation.reports
		 WHERE reporter_id = $1::uuid
		 ORDER BY created_at DESC
		 LIMIT $2`, p.UserID, MaxQueueSize)
	if err != nil {
		return nil, fmt.Errorf("moderation: list mine: %w", err)
	}
	defer rows.Close()
	return scanReports(rows, false)
}

// Queue returns reports for a moderator. An empty status means every open and reviewing
// report; "all" means every report.
func (s *Service) Queue(ctx context.Context, status string, limit int) ([]Report, error) {
	status = strings.TrimSpace(status)
	if limit <= 0 || limit > MaxQueueSize {
		limit = MaxQueueSize
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, reporter_id::text, subject_type, subject_id::text, reason, details,
		       status, created_at, updated_at, resolved_at, COALESCE(resolved_by::text, '')
		  FROM moderation.reports
		 WHERE ($1 = '' AND status IN ('open', 'reviewing'))
		    OR ($1 = 'all')
		    OR ($1 <> '' AND $1 <> 'all' AND status = $1)
		 ORDER BY CASE status WHEN 'open' THEN 0 WHEN 'reviewing' THEN 1 ELSE 2 END,
		          created_at ASC
		 LIMIT $2`, status, limit)
	if err != nil {
		return nil, fmt.Errorf("moderation: queue: %w", err)
	}
	defer rows.Close()
	return scanReports(rows, false)
}

// Detail returns one report with its full trail. A reporter may read their own report; any
// caller holding the safety permission may read any report.
func (s *Service) Detail(ctx context.Context, p identity.Principal, id string, anyReport bool) (Report, error) {
	var rep Report
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, reporter_id::text, subject_type, subject_id::text, reason, details,
		       status, created_at, updated_at, resolved_at, COALESCE(resolved_by::text, '')
		  FROM moderation.reports WHERE id = $1::uuid`, id).Scan(
		&rep.ID, &rep.ReporterID, &rep.SubjectType, &rep.SubjectID, &rep.Reason, &rep.Details,
		&rep.Status, &rep.CreatedAt, &rep.UpdatedAt, &rep.ResolvedAt, &rep.ResolvedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return Report{}, ErrNotFound
	}
	if err != nil {
		return Report{}, fmt.Errorf("moderation: detail: %w", err)
	}
	if !anyReport && rep.ReporterID != p.UserID {
		return Report{}, ErrForbidden
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, action, COALESCE(from_status, ''), to_status, note,
		       COALESCE(actor_id::text, ''), created_at
		  FROM moderation.report_events
		 WHERE report_id = $1::uuid
		 ORDER BY id`, id)
	if err != nil {
		return Report{}, fmt.Errorf("moderation: list events: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.Action, &e.FromStatus, &e.ToStatus, &e.Note,
			&e.ActorID, &e.CreatedAt); err != nil {
			return Report{}, fmt.Errorf("moderation: scan event: %w", err)
		}
		rep.Events = append(rep.Events, e)
	}
	return rep, rows.Err()
}

// Transition moves a report and appends the reason for the move. The database does the
// rest: the event table rejects edits, so this is the only way history is written.
func (s *Service) Transition(ctx context.Context, actor identity.Principal, id, action, note string) (Report, error) {
	action = strings.TrimSpace(action)
	note = strings.TrimSpace(note)
	if len([]rune(note)) > MaxNoteLength {
		return Report{}, fmt.Errorf("%w: notes are limited to %d characters", ErrInvalidInput, MaxNoteLength)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("moderation: begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var current string
	err = tx.QueryRow(ctx, `SELECT status FROM moderation.reports WHERE id = $1::uuid FOR UPDATE`,
		id).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return Report{}, ErrNotFound
	}
	if err != nil {
		return Report{}, fmt.Errorf("moderation: lock report: %w", err)
	}

	next, needsNote, ok := transition(current, action)
	if !ok {
		return Report{}, fmt.Errorf("%w: %s from %s", ErrConflict, action, current)
	}
	if needsNote && len([]rune(note)) < 3 {
		return Report{}, fmt.Errorf("%w: a decision needs a short note saying why", ErrInvalidInput)
	}

	closing := next == StatusActioned || next == StatusDismissed
	if _, err := tx.Exec(ctx, `
		UPDATE moderation.reports
		   SET status = $2,
		       updated_at = now(),
		       resolved_at = CASE WHEN $3 THEN now() ELSE NULL END,
		       resolved_by = CASE WHEN $3 THEN $4::uuid ELSE NULL END
		 WHERE id = $1::uuid`, id, next, closing, actor.UserID); err != nil {
		return Report{}, fmt.Errorf("moderation: update status: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO moderation.report_events (report_id, actor_id, action, from_status, to_status, note)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6)`,
		id, actor.UserID, action, current, next, note); err != nil {
		return Report{}, fmt.Errorf("moderation: insert event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Report{}, fmt.Errorf("moderation: commit: %w", err)
	}

	s.logger.InfoContext(ctx, "report transitioned",
		slog.String("report_id", id),
		slog.String("from", current),
		slog.String("to", next),
		slog.String("action", action),
		slog.String("actor_id", actor.UserID))

	return s.Detail(ctx, actor, id, true)
}

// transition returns the target status for an action, whether a note is required, and
// whether the action is legal from the current status.
func transition(current, action string) (next string, needsNote, ok bool) {
	switch action {
	case ActionStartReview:
		if current != StatusOpen {
			return "", false, false
		}
		return StatusReviewing, false, true
	case ActionAction:
		if current != StatusOpen && current != StatusReviewing {
			return "", false, false
		}
		return StatusActioned, true, true
	case ActionDismiss:
		if current != StatusOpen && current != StatusReviewing {
			return "", false, false
		}
		return StatusDismissed, true, true
	case ActionReopen:
		if current != StatusActioned && current != StatusDismissed {
			return "", false, false
		}
		return StatusReviewing, true, true
	default:
		return "", false, false
	}
}

// subjectVisible answers "may this person report that thing at all", which also keeps the
// endpoint from confirming that an arbitrary id exists.
func (s *Service) subjectVisible(ctx context.Context, tx pgx.Tx, userID, subjectType, subjectID string) (bool, error) {
	var exists bool
	switch subjectType {
	case SubjectUser:
		err := tx.QueryRow(ctx, `SELECT true FROM identity.users WHERE id = $1::uuid`,
			subjectID).Scan(&exists)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return exists, err
	case SubjectPost:
		// A post is reportable when it is published; a draft is nobody's business yet.
		err := tx.QueryRow(ctx, `SELECT true FROM creator.posts WHERE id = $1::uuid AND status = 'published'`,
			subjectID).Scan(&exists)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return exists, err
	case SubjectMessage:
		// Only a participant can see a message, so only a participant can report it.
		err := tx.QueryRow(ctx, `
			SELECT true
			  FROM messaging.messages m
			  JOIN messaging.conversations c ON c.id = m.conversation_id
			 WHERE m.id = $1::uuid AND ($2::uuid = c.fan_id OR $2::uuid = c.creator_id)`,
			subjectID, userID).Scan(&exists)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return exists, err
	default:
		return false, nil
	}
}

func scanReports(rows pgx.Rows, withEvents bool) ([]Report, error) {
	reports := make([]Report, 0, 16)
	for rows.Next() {
		var rep Report
		if err := rows.Scan(&rep.ID, &rep.ReporterID, &rep.SubjectType, &rep.SubjectID,
			&rep.Reason, &rep.Details, &rep.Status, &rep.CreatedAt, &rep.UpdatedAt,
			&rep.ResolvedAt, &rep.ResolvedBy); err != nil {
			return nil, fmt.Errorf("moderation: scan report: %w", err)
		}
		reports = append(reports, rep)
	}
	return reports, rows.Err()
}
