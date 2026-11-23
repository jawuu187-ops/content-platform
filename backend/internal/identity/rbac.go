package identity

// Role-based access control.
//
// Roles are stored as explicit grants (identity.role_grants). Permissions are
// derived from roles in code, never stored, so changing the matrix is a review
// and a deploy instead of a data migration that could grant access by mistake.
//
// The matrix is deliberately small and explicit. Every permission names a real
// capability; none of them is "logged in".

// Role names. These strings are constrained by a CHECK in the identity schema.
const (
	RoleFan         = "fan"
	RoleCreator     = "creator"
	RoleCreatorTeam = "creator_team"
	RoleModerator   = "moderator"
	RoleFinance     = "finance"
	RoleSupport     = "support"
	RoleAdmin       = "admin"
)

// Permission names a capability.
type Permission string

const (
	// Content
	PermContentRead     Permission = "content.read"     // view entitled content
	PermContentWrite    Permission = "content.write"    // upload and edit own content
	PermContentPublish  Permission = "content.publish"  // publish or schedule
	PermContentModerate Permission = "content.moderate" // review reported content

	// Accounts
	PermUsersRead  Permission = "users.read"
	PermUsersWrite Permission = "users.write"
	PermRolesGrant Permission = "roles.grant"

	// Money
	PermFinanceRead        Permission = "finance.read"
	PermPayoutRequest      Permission = "payouts.request"
	PermPayoutApprove      Permission = "payouts.approve"
	PermSubscriptionManage Permission = "subscriptions.manage"

	// Messaging and safety
	PermMessagesSend Permission = "messages.send"
	PermSafetyReview Permission = "safety.review"
	PermSupportView  Permission = "support.view"

	// Platform
	PermAdminAll Permission = "admin.all"
)

// AllPermissions lists every permission. Tests iterate this, so a permission added
// without a matrix entry fails the build's tests instead of shipping unprotected.
var AllPermissions = []Permission{
	PermContentRead, PermContentWrite, PermContentPublish, PermContentModerate,
	PermUsersRead, PermUsersWrite, PermRolesGrant,
	PermFinanceRead, PermPayoutRequest, PermPayoutApprove, PermSubscriptionManage,
	PermMessagesSend, PermSafetyReview, PermSupportView,
	PermAdminAll,
}

// rolePermissions is the single source of truth for the matrix.
//
// Rationale for the non-obvious rows:
//   - fan gets content.read and messages.send only. A subscriber must never be
//     able to write creator content or see another account's financials.
//   - creator_team can write and publish on behalf of a creator but cannot
//     request payouts: money movement stays with the account owner.
//   - moderator can read users and moderate content but cannot change roles or
//     read financials.
//   - finance approves payouts and reads financials, but has no content powers.
//   - support sees account metadata for tickets and nothing else.
var rolePermissions = map[string][]Permission{
	RoleFan: {
		PermContentRead,
		PermMessagesSend,
	},
	RoleCreator: {
		PermContentRead, PermContentWrite, PermContentPublish,
		PermFinanceRead, PermPayoutRequest, PermSubscriptionManage,
		PermMessagesSend,
	},
	RoleCreatorTeam: {
		PermContentRead, PermContentWrite, PermContentPublish,
		PermMessagesSend,
	},
	RoleModerator: {
		PermContentRead, PermContentModerate,
		PermUsersRead, PermUsersWrite, PermSafetyReview,
	},
	RoleFinance: {
		PermFinanceRead, PermPayoutApprove,
		PermUsersRead,
	},
	RoleSupport: {
		PermUsersRead, PermSupportView,
	},
	RoleAdmin: {
		PermAdminAll,
	},
}

// PermissionsFor returns the deduplicated union of permissions for a set of roles.
func PermissionsFor(roles []string) []Permission {
	seen := make(map[Permission]bool, len(AllPermissions))
	out := make([]Permission, 0, len(AllPermissions))

	add := func(p Permission) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}

	for _, role := range roles {
		if role == RoleAdmin {
			// Admin is an explicit superset rather than an implicit one: the
			// permission exists in the matrix and every check tests for it.
			for _, p := range AllPermissions {
				add(p)
			}
			continue
		}
		for _, p := range rolePermissions[role] {
			add(p)
		}
	}
	return out
}

// HasPermission reports whether any of the roles grants the permission.
func HasPermission(roles []string, want Permission) bool {
	for _, role := range roles {
		if role == RoleAdmin {
			return true
		}
		for _, p := range rolePermissions[role] {
			if p == want {
				return true
			}
		}
	}
	return false
}

// PermissionStrings renders permissions for JSON responses.
func PermissionStrings(roles []string) []string {
	perms := PermissionsFor(roles)
	out := make([]string, 0, len(perms))
	for _, p := range perms {
		out = append(out, string(p))
	}
	return out
}

// IsKnownRole reports whether a role name is part of the matrix.
func IsKnownRole(role string) bool {
	_, ok := rolePermissions[role]
	return ok
}
