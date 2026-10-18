package workspace

import (
	"errors"
	"testing"
)

func TestRequireConsentGrantNeedsEveryPermissionOfTheConsentCapability(t *testing.T) {
	full := heldPermissions{"leads:read": true, "leads:update": true, "leads:consent": true}
	cases := []struct {
		name    string
		checker PermissionChecker
		by      Requester
		allowed bool
	}{
		{"read, update and consent", full, Requester{UserID: "u"}, true},
		{"no consent", heldPermissions{"leads:read": true, "leads:update": true}, Requester{UserID: "u"}, false},
		{"consent without update", heldPermissions{"leads:read": true, "leads:consent": true}, Requester{UserID: "u"}, false},
		{"nobody", full, Requester{UserID: " "}, false},
		{"no checker", nil, Requester{UserID: "u"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := RequireConsentGrant(tc.checker, "ws", tc.by)
			if tc.allowed && err != nil {
				t.Fatalf("err = %v", err)
			}
			if !tc.allowed && !errors.Is(err, ErrConsentGrantForbidden) {
				t.Fatalf("err = %v, want ErrConsentGrantForbidden", err)
			}
		})
	}
}

func TestRequireConsentGrantPassesTheSystemAdminFlagToTheChecker(t *testing.T) {
	var sawAdmin bool
	checker := adminOnly(func(isAdmin bool) bool { sawAdmin = isAdmin; return isAdmin })
	if err := RequireConsentGrant(checker, "ws", Requester{UserID: "root", SystemAdmin: true}); err != nil || !sawAdmin {
		t.Fatalf("err = %v, saw admin %v", err, sawAdmin)
	}
	if err := RequireConsentGrant(checker, "ws", Requester{UserID: "root"}); !errors.Is(err, ErrConsentGrantForbidden) {
		t.Fatalf("err = %v", err)
	}
}

func TestTheConsentCapabilityIsInTheCatalog(t *testing.T) {
	if _, ok := CapabilityByKey(CapabilityLeadConsent); !ok {
		t.Fatalf("%s is missing from the catalog", CapabilityLeadConsent)
	}
	if CodeConsentGrantForbidden != "lead_consent_grant_forbidden" {
		t.Fatalf("code = %s", CodeConsentGrantForbidden)
	}
}

type adminOnly func(isAdmin bool) bool

func (a adminOnly) HasWorkspacePermission(_, _, _, _ string, isAdmin bool) bool {
	return a(isAdmin)
}
