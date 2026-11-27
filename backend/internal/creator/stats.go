package creator

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Studio audience statistics.
//
// Every number is a count of rows that belong to this creator and describe real
// access:
//
//   - Members are live paid memberships: creator.subscriptions rows in status
//     'active' whose current period has not ended. Distinct fans, so a renewal or
//     a second row cannot inflate the count.
//   - Support grants are excluded. An entitlement with source 'admin' or 'grant'
//     is access issued by hand, not a member, and including it would make a
//     disposable demo fixture look like an audience.
//   - Unlocks are paid one-off unlocks (source 'purchase') on this creator's posts,
//     granted in the last seven days and not revoked. A refunded unlock and an
//     admin grant are not demand.
//   - Tier share is only defined when there is at least one member; with none,
//     TopTier is nil rather than a fabricated 0%.
//
// The creator id is the authenticated account's own, taken from the session by the
// handler; there is no parameter with which to read someone else's numbers.

// TierStat is the audience of one subscription tier.
type TierStat struct {
	TierID   string `json:"tier_id"`
	Name     string `json:"name"`
	Members  int    `json:"members"`
	ShareBPS int    `json:"share_bps"`
}

// Stats is the creator's own audience picture.
type Stats struct {
	CreatorID string     `json:"creator_id"`
	Members   int        `json:"members"`
	Unlocks7d int        `json:"unlocks_7d"`
	Tiers     []TierStat `json:"tiers"`
	// TopTier is the active tier with the most members, ties broken by the tier's
	// own order. Nil when the creator has no live members.
	TopTier *TierStat `json:"top_tier,omitempty"`
}

// Stats reports the audience of the caller's own creator profile.
func (s *Service) Stats(ctx context.Context, creatorID string) (Stats, error) {
	if creatorID == "" {
		return Stats{}, fmt.Errorf("%w: a creator is required", ErrForbidden)
	}
	// Only a real creator profile has an audience; a fan asking gets a refusal
	// rather than a page of zeros that pretends to be their own studio.
	var exists bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM creator.profiles WHERE user_id = $1::uuid)`, creatorID).Scan(&exists)
	if err != nil {
		return Stats{}, fmt.Errorf("creator: check profile: %w", err)
	}
	if !exists {
		return Stats{}, fmt.Errorf("%w: a creator profile is required", ErrForbidden)
	}

	now := s.now()
	out := Stats{CreatorID: creatorID, Tiers: []TierStat{}}

	// Members and their tiers come from one query so the total and the split
	// cannot disagree. The period check makes an active row whose period has
	// already ended count as lapsed, even before the expiry job touches it.
	rows, err := s.pool.Query(ctx, `
		SELECT t.id::text, t.name, COUNT(DISTINCT s.fan_id)::int
		FROM creator.subscriptions s
		JOIN creator.tiers t ON t.id = s.tier_id
		WHERE s.creator_id = $1::uuid
		  AND s.status = 'active'
		  AND s.current_period_end > $2
		GROUP BY t.id, t.name, t.sort_order
		ORDER BY COUNT(DISTINCT s.fan_id) DESC, t.sort_order ASC, t.id ASC`, creatorID, now)
	if err != nil {
		return Stats{}, fmt.Errorf("creator: stats by tier: %w", err)
	}
	defer rows.Close()

	members := 0
	for rows.Next() {
		var tier TierStat
		if err := rows.Scan(&tier.TierID, &tier.Name, &tier.Members); err != nil {
			return Stats{}, fmt.Errorf("creator: scan tier stats: %w", err)
		}
		members += tier.Members
		out.Tiers = append(out.Tiers, tier)
	}
	if err := rows.Err(); err != nil {
		return Stats{}, fmt.Errorf("creator: iterate tier stats: %w", err)
	}
	out.Members = members

	// Share is in basis points so the client formats it without inventing a
	// rounding rule, and only the tier split is rounded.
	if members > 0 {
		for i := range out.Tiers {
			out.Tiers[i].ShareBPS = out.Tiers[i].Members * 10_000 / members
		}
		top := out.Tiers[0]
		out.TopTier = &top
	}

	if err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*)::int
		FROM creator.post_unlocks u
		JOIN creator.posts p ON p.id = u.post_id
		WHERE p.creator_id = $1::uuid
		  AND u.source = 'purchase'
		  AND u.revoked_at IS NULL
		  AND u.granted_at > $2`, creatorID, now.AddDate(0, 0, -7)).Scan(&out.Unlocks7d); err != nil &&
		!errors.Is(err, pgx.ErrNoRows) {
		return Stats{}, fmt.Errorf("creator: count unlocks: %w", err)
	}
	return out, nil
}
