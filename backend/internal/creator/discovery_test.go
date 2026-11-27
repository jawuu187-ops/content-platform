package creator

import (
	"context"
	"testing"
)

func TestDiscoveryFiltersAndPaginates(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	for i, category := range []string{"art", "music", "art", "fitness"} {
		p := h.account(t)
		handle := "page" + string(rune('a'+i)) + string(rune('0'+i))
		if _, err := h.svc.CreateProfile(ctx, p, ProfileInput{
			Handle: handle, DisplayName: "Creator " + handle, Category: category, Publish: ptrBool(true),
		}, ""); err != nil {
			t.Fatal(err)
		}
	}
	page, next, err := h.svc.ListPublishedPage(ctx, DiscoveryQuery{Category: "art", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 1 || next != 1 {
		t.Fatalf("first page = %d next=%d", len(page), next)
	}
	page2, next2, err := h.svc.ListPublishedPage(ctx, DiscoveryQuery{Category: "art", Limit: 1, Offset: next})
	if err != nil {
		t.Fatal(err)
	}
	if len(page2) != 1 || page2[0].UserID == page[0].UserID || next2 != -1 {
		t.Fatalf("second page = %+v next=%d", page2, next2)
	}
	search, _, err := h.svc.ListPublishedPage(ctx, DiscoveryQuery{Text: "pageb"})
	if err != nil {
		t.Fatal(err)
	}
	if len(search) != 1 || search[0].Category != "music" {
		t.Fatalf("search = %+v", search)
	}
}
