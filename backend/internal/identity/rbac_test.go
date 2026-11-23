package identity

import "testing"

// The full matrix, written out independently of the implementation. If the code
// and this table disagree, one of them is wrong and the build fails.
var expectedMatrix = map[string]map[Permission]bool{
	RoleFan: {
		PermContentRead: true, PermMessagesSend: true,
	},
	RoleCreator: {
		PermContentRead: true, PermContentWrite: true, PermContentPublish: true,
		PermFinanceRead: true, PermPayoutRequest: true, PermSubscriptionManage: true,
		PermMessagesSend: true,
	},
	RoleCreatorTeam: {
		PermContentRead: true, PermContentWrite: true, PermContentPublish: true,
		PermMessagesSend: true,
	},
	RoleModerator: {
		PermContentRead: true, PermContentModerate: true,
		PermUsersRead: true, PermUsersWrite: true, PermSafetyReview: true,
	},
	RoleFinance: {
		PermFinanceRead: true, PermPayoutApprove: true, PermUsersRead: true,
	},
	RoleSupport: {
		PermUsersRead: true, PermSupportView: true,
	},
}

func TestPermissionMatrixForEveryRoleAndPermission(t *testing.T) {
	for role, wants := range expectedMatrix {
		for _, perm := range AllPermissions {
			want := wants[perm]
			if got := HasPermission([]string{role}, perm); got != want {
				t.Fatalf("role %q permission %q: got %v want %v", role, perm, got, want)
			}
		}
	}
}

func TestAdminIsAnExplicitSuperset(t *testing.T) {
	for _, perm := range AllPermissions {
		if !HasPermission([]string{RoleAdmin}, perm) {
			t.Fatalf("admin must hold %q", perm)
		}
	}
	if len(PermissionsFor([]string{RoleAdmin})) != len(AllPermissions) {
		t.Fatalf("admin should hold every permission, got %d of %d",
			len(PermissionsFor([]string{RoleAdmin})), len(AllPermissions))
	}
}

func TestDeniedPermissionsAreExactlyTheMissingOnes(t *testing.T) {
	// The permissions a fan must never hold. Tested explicitly so a later edit to
	// the matrix cannot quietly widen what a paying subscriber can do.
	denied := []Permission{
		PermContentWrite, PermContentPublish, PermContentModerate,
		PermUsersRead, PermUsersWrite, PermRolesGrant,
		PermFinanceRead, PermPayoutRequest, PermPayoutApprove,
		PermSubscriptionManage, PermSafetyReview, PermSupportView, PermAdminAll,
	}
	for _, perm := range denied {
		if HasPermission([]string{RoleFan}, perm) {
			t.Fatalf("fan must not hold %q", perm)
		}
	}

	// A creator must not be able to approve its own payouts.
	if HasPermission([]string{RoleCreator}, PermPayoutApprove) {
		t.Fatal("creator must not hold payouts.approve")
	}
	// Support must not read financials or moderate content.
	if HasPermission([]string{RoleSupport}, PermFinanceRead) {
		t.Fatal("support must not read financials")
	}
	if HasPermission([]string{RoleSupport}, PermContentModerate) {
		t.Fatal("support must not moderate content")
	}
	// Moderation must not grant roles.
	if HasPermission([]string{RoleModerator}, PermRolesGrant) {
		t.Fatal("moderator must not grant roles")
	}
}

func TestUnionAcrossRoles(t *testing.T) {
	both := []string{RoleCreator, RoleModerator}
	if !HasPermission(both, PermContentPublish) || !HasPermission(both, PermContentModerate) {
		t.Fatal("union of creator and moderator must include both capabilities")
	}
	if HasPermission(both, PermPayoutApprove) {
		t.Fatal("union must not invent permissions no role holds")
	}

	perms := PermissionStrings(both)
	seen := map[string]int{}
	for _, p := range perms {
		seen[p]++
	}
	for p, n := range seen {
		if n != 1 {
			t.Fatalf("permission %q listed %d times; union must be deduplicated", p, n)
		}
	}
}

func TestUnknownRoleGrantsNothing(t *testing.T) {
	if HasPermission([]string{"superuser"}, PermContentRead) {
		t.Fatal("an unknown role must not grant anything")
	}
	if IsKnownRole("superuser") {
		t.Fatal("IsKnownRole accepted an unknown role")
	}
	if got := PermissionStrings([]string{"superuser"}); len(got) != 0 {
		t.Fatalf("unknown role produced permissions: %v", got)
	}
	if len(PermissionsFor(nil)) != 0 {
		t.Fatal("no roles must produce no permissions")
	}
}

func TestEveryPermissionBelongsToAtLeastOneRole(t *testing.T) {
	// A permission nobody holds is either dead code or a matrix omission.
	for _, perm := range AllPermissions {
		if perm == PermAdminAll {
			continue
		}
		held := false
		for _, role := range []string{RoleFan, RoleCreator, RoleCreatorTeam, RoleModerator, RoleFinance, RoleSupport} {
			if HasPermission([]string{role}, perm) {
				held = true
				break
			}
		}
		if !held {
			t.Fatalf("permission %q is not granted to any non-admin role", perm)
		}
	}
}
