package identity

import (
	"context"
	"fmt"
)

// GrantRole gives an account an additional role. It is the only write path to
// identity.role_grants outside registration, and every call is audited with the
// acting principal so privilege changes can be reviewed later.
func (s *Service) GrantRole(ctx context.Context, actor Principal, userID, role, requestID string) ([]string, error) {
	if !IsKnownRole(role) {
		return nil, fmt.Errorf("%w: unknown role %q", ErrInvalidInput, role)
	}
	if userID == "" {
		return nil, fmt.Errorf("%w: user_id is required", ErrInvalidInput)
	}

	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("identity: begin grant role: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// The user must exist: a role grant for a missing account is a bug, not a
	// silent no-op.
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT true FROM identity.users WHERE id = $1::uuid AND deleted_at IS NULL`, userID).
		Scan(&exists); err != nil {
		return nil, ErrNotFound
	}
	if err := s.store.grantRole(ctx, tx, userID, role, actor.UserID); err != nil {
		return nil, err
	}
	s.store.audit(ctx, tx, "user", actor.UserID, "role.granted", "user", userID, requestID)
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("identity: commit grant role: %w", err)
	}
	return s.store.roles(ctx, userID)
}

// RolesOf returns the roles held by an account.
func (s *Service) RolesOf(ctx context.Context, userID string) ([]string, error) {
	roles, err := s.store.roles(ctx, userID)
	if err != nil {
		return nil, err
	}
	if roles == nil {
		roles = []string{}
	}
	return roles, nil
}

// BecomeCreator is the one self-service role grant: an account may make itself a
// creator, which adds publishing capability but no moderation, finance or admin
// power. Every other grant stays with an administrator.
func (s *Service) BecomeCreator(ctx context.Context, p Principal, requestID string) ([]string, error) {
	if p.UserID == "" {
		return nil, ErrInvalidCredentials
	}
	tx, err := s.store.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("identity: begin become creator: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := s.store.userByIDTx(ctx, tx, p.UserID); err != nil {
		return nil, err
	}
	if err := s.store.grantRole(ctx, tx, p.UserID, RoleCreator, p.UserID); err != nil {
		return nil, err
	}
	s.store.audit(ctx, tx, "user", p.UserID, "role.self_granted", "user", p.UserID, requestID)
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("identity: commit become creator: %w", err)
	}
	return s.store.roles(ctx, p.UserID)
}
