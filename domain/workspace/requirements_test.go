package workspace

import (
	"errors"
	"testing"
)

type heldPermissions map[string]bool

func (h heldPermissions) HasWorkspacePermission(userID, _, resource, action string, _ bool) bool {
	return userID != "" && h[resource+":"+action]
}

func TestCapabilitiesRequireJoinsCapabilitiesWithoutRepeats(t *testing.T) {
	entries, err := CapabilitiesRequire([]CapabilityKey{"leads.export", "leads.read_addresses"})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]int{}
	for _, e := range entries {
		seen[e.Key()]++
	}
	for _, key := range []string{"leads:read", "leads:export", "reports:create", "reports:read", "leads:read_addresses"} {
		if seen[key] != 1 {
			t.Fatalf("%s appears %d times in %v", key, seen[key], entries)
		}
	}
	if _, err := CapabilitiesRequire([]CapabilityKey{"nope.never"}); !errors.Is(err, ErrUnknownCapability) {
		t.Fatalf("an unknown capability = %v", err)
	}
}

func TestHoldsAllNeedsEveryEntryAndAPerson(t *testing.T) {
	read := PermissionEntry{Resource: ResourceLeads, Action: ActionRead}
	export := PermissionEntry{Resource: ResourceLeads, Action: ActionExport}
	held := heldPermissions{"leads:read": true}
	cases := []struct {
		name    string
		checker PermissionChecker
		userID  string
		entries []PermissionEntry
		want    bool
	}{
		{"every entry held", held, "u", []PermissionEntry{read}, true},
		{"one entry missing", held, "u", []PermissionEntry{read, export}, false},
		{"nothing required", held, "u", nil, false},
		{"nobody", held, " ", []PermissionEntry{read}, false},
		{"no checker", nil, "u", []PermissionEntry{read}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HoldsAll(tc.checker, "ws", tc.userID, false, tc.entries); got != tc.want {
				t.Fatalf("HoldsAll = %v, want %v", got, tc.want)
			}
		})
	}
}
