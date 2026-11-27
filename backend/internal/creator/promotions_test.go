package creator

import (
	"context"
	"testing"
	"time"
)

func TestFreeTrialAndPromotion(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	owner, profile, _ := h.creator(t)
	trial := 7
	tier, err := h.svc.CreateTier(ctx, owner, TierInput{
		Name: "Trial tier", PriceMinor: ptrInt64(2000), Currency: "USD",
		Description: "Try it", TrialDays: &trial,
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	fan := h.account(t)
	sub, err := h.svc.SubscribeWithOffer(ctx, fan, profile.Handle, tier.ID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !sub.IsTrial || sub.PriceMinor != 0 || sub.DiscountMinor != 2000 {
		t.Fatalf("trial subscription = %+v", sub)
	}
	if sub.PeriodStart == nil || sub.PeriodEnd == nil ||
		sub.PeriodEnd.Sub(*sub.PeriodStart) < 6*24*time.Hour {
		t.Fatalf("trial period = %+v", sub)
	}

	// A different tier without a trial, with a one-use promotion.
	paid, err := h.svc.CreateTier(ctx, owner, TierInput{
		Name: "Paid tier", PriceMinor: ptrInt64(5000), Currency: "USD", Description: "Paid",
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Now().UTC().Add(24 * time.Hour)
	promo, err := h.svc.CreatePromotion(ctx, owner, PromotionInput{
		Code: "LAUNCH20", PercentOff: 20, MaxRedemptions: 1, ExpiresAt: &expires,
	})
	if err != nil {
		t.Fatal(err)
	}
	fan2 := h.account(t)
	discounted, err := h.svc.SubscribeWithOffer(ctx, fan2, profile.Handle, paid.ID, "launch20", "")
	if err != nil {
		t.Fatal(err)
	}
	if discounted.PriceMinor != 4000 || discounted.DiscountMinor != 1000 || discounted.PromotionCode != "LAUNCH20" {
		t.Fatalf("discounted = %+v", discounted)
	}
	fan3 := h.account(t)
	if _, err := h.svc.SubscribeWithOffer(ctx, fan3, profile.Handle, paid.ID, "LAUNCH20", ""); err == nil {
		t.Fatal("exhausted promotion was accepted")
	}
	items, err := h.svc.Promotions(ctx, owner)
	if err != nil || len(items) != 1 || items[0].RedeemedCount != 1 || items[0].ID != promo.ID {
		t.Fatalf("promotions = %+v err=%v", items, err)
	}
}
