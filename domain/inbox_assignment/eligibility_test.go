package inbox_assignment

import "testing"

type fakeAuthz struct {
	ownerOrAdmin map[string]bool
	roulette     map[string]bool

	askedResource string
	askedAction   string
}

func (a *fakeAuthz) HasWorkspacePermission(userID, workspaceID, resource, action string, isSystemAdmin bool) bool {
	a.askedResource, a.askedAction = resource, action
	return a.roulette[userID]
}

func (a *fakeAuthz) IsWorkspaceOwnerOrAdmin(userID, workspaceID string) bool {
	return a.ownerOrAdmin[userID]
}

func TestCanReceiveRoulette(t *testing.T) {
	cases := []struct {
		name         string
		userID       string
		ownerOrAdmin bool
		hasRoulette  bool
		skipAdmins   bool
		want         bool
	}{
		{"member with the permission", "u", false, true, false, true},
		{"member without the permission", "u", false, false, false, false},
		{"admin with the permission, admins included", "u", true, true, false, true},
		{"admin with the permission, admins skipped", "u", true, true, true, false},
		{"admin without the permission, admins included", "u", true, false, false, false},
		{"member with the permission, admins skipped", "u", false, true, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			authz := &fakeAuthz{
				ownerOrAdmin: map[string]bool{tc.userID: tc.ownerOrAdmin},
				roulette:     map[string]bool{tc.userID: tc.hasRoulette},
			}
			if got := CanReceiveRoulette(authz, tc.userID, "ws-1", tc.skipAdmins); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestCanReceiveRoulette_AsksForTheRoulettePermission(t *testing.T) {
	authz := &fakeAuthz{roulette: map[string]bool{"u": true}}
	CanReceiveRoulette(authz, "u", "ws-1", false)
	if authz.askedResource != RouletteResource || authz.askedAction != RouletteAction {
		t.Fatalf("asked for %s:%s, want %s:%s", authz.askedResource, authz.askedAction, RouletteResource, RouletteAction)
	}
}

func TestCanReceiveRoulette_MissingInputs(t *testing.T) {
	authz := &fakeAuthz{roulette: map[string]bool{"u": true}}
	if CanReceiveRoulette(nil, "u", "ws-1", false) {
		t.Fatal("nil authorizer must not grant the roulette")
	}
	if CanReceiveRoulette(authz, "", "ws-1", false) {
		t.Fatal("empty user must not grant the roulette")
	}
	if CanReceiveRoulette(authz, "u", "", false) {
		t.Fatal("empty workspace must not grant the roulette")
	}
}
