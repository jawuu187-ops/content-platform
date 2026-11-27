package identity

import (
	"context"
)

// VerifyPassword re-authenticates the current account for account deletion or
// another irreversible action. It never creates a session and never exposes the
// stored hash.
func (s *Service) VerifyPassword(ctx context.Context, userID, password string) error {
	hash, err := s.store.passwordHashFor(ctx, userID)
	if err != nil {
		return err
	}
	ok, _ := s.hasher.Verify(password, hash)
	if !ok {
		return ErrInvalidCredentials
	}
	return nil
}
