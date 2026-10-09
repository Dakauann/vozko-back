package workspace

import "testing"

func TestOwnerOrPlatformAdmin(t *testing.T) {
	tests := []struct {
		name          string
		userID        string
		ownerID       string
		platformAdmin bool
		want          bool
	}{
		{"the owner", "owner-1", "owner-1", false, true},
		{"the owner with stray spaces", " owner-1 ", "owner-1 ", false, true},
		{"a platform admin", "staff-1", "owner-1", true, true},
		{"another member", "member-1", "owner-1", false, false},
		{"nobody", "", "owner-1", false, false},
		{"a workspace without an owner", "", "", false, false},
		{"a platform admin without an id", "", "owner-1", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := OwnerOrPlatformAdmin(tt.userID, tt.ownerID, tt.platformAdmin); got != tt.want {
				t.Fatalf("OwnerOrPlatformAdmin() = %v, want %v", got, tt.want)
			}
		})
	}
}
