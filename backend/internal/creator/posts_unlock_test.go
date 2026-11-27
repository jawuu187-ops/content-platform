package creator

import (
	"context"
	"errors"
	"testing"
)

// The simulated provider may grant a PPV unlock, but it must do so through one
// durable unlock row and must never be confused with a real provider being
// configured. A service without fake payments refuses before writing anything.
func TestUnlockPostRequiresAPaymentProvider(t *testing.T) {
	h := newHarness(t)
	owner, _, _ := h.creator(t)
	fan := h.account(t)

	post, err := h.svc.CreatePost(context.Background(), owner, PostInput{
		Body: "A private set", Visibility: VisibilityPPV,
		PriceMinor: ptrInt64(500), Currency: "USD", Publish: ptrBool(true),
	}, "")
	if err != nil {
		t.Fatalf("create PPV post: %v", err)
	}

	unlock, err := h.svc.UnlockPost(context.Background(), fan, post.ID, "")
	if err != nil {
		t.Fatalf("simulated unlock: %v", err)
	}
	if unlock.PostID != post.ID {
		t.Fatalf("unlock post = %q, want %q", unlock.PostID, post.ID)
	}
	again, err := h.svc.UnlockPost(context.Background(), fan, post.ID, "")
	if err != nil {
		t.Fatalf("repeat simulated unlock: %v", err)
	}
	if again.ID != unlock.ID {
		t.Fatalf("repeat unlock created a second row: %q vs %q", again.ID, unlock.ID)
	}

	real := New(h.svc.pool, WithAgeGate(stubGate{allow: true}))
	if _, err := real.UnlockPost(context.Background(), fan, post.ID, ""); !errors.Is(err, ErrPaymentRequired) {
		t.Fatalf("unconfigured provider must refuse with ErrPaymentRequired, got %v", err)
	}

	blocked := New(h.svc.pool, WithFakePayments(true), WithAgeGate(stubGate{allow: false}))
	if _, err := blocked.UnlockPost(context.Background(), fan, post.ID, ""); !errors.Is(err, ErrAgeRequired) {
		t.Fatalf("unverified account must be refused with ErrAgeRequired, got %v", err)
	}
}
