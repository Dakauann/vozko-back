package shared

import "testing"

func TestOwned(t *testing.T) {
	cases := []struct {
		name    string
		owned   Owned
		viewer  string
		canRead bool
		canEdit bool
	}{
		{"owner of a private item", Owned{OwnerID: "u1", Visibility: VisibilityPrivate}, "u1", true, true},
		{"another member on a private item", Owned{OwnerID: "u1", Visibility: VisibilityPrivate}, "u2", false, false},
		{"another member on a shared item", Owned{OwnerID: "u1", Visibility: VisibilityShared}, "u2", true, false},
		{"owner of a shared item", Owned{OwnerID: "u1", Visibility: VisibilityShared}, "u1", true, true},
		{"anonymous viewer on a shared item", Owned{OwnerID: "u1", Visibility: VisibilityShared}, " ", false, false},
		{"ownerless item never matches an anonymous viewer", Owned{Visibility: VisibilityPrivate}, "", false, false},
		{"unknown visibility reads as private", Owned{OwnerID: "u1", Visibility: "public"}, "u2", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.owned.CanRead(tc.viewer); got != tc.canRead {
				t.Errorf("CanRead = %v, want %v", got, tc.canRead)
			}
			if got := tc.owned.CanEdit(tc.viewer); got != tc.canEdit {
				t.Errorf("CanEdit = %v, want %v", got, tc.canEdit)
			}
		})
	}
}

func TestVisibilityValid(t *testing.T) {
	for v, want := range map[Visibility]bool{VisibilityPrivate: true, VisibilityShared: true, "": false, "public": false} {
		if got := v.Valid(); got != want {
			t.Errorf("%q.Valid() = %v, want %v", v, got, want)
		}
	}
}
