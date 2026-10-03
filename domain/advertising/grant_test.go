package advertising

import (
	"errors"
	"testing"
	"time"
)

func fullGrant() *Grant {
	return &Grant{Status: GrantActive, AccessToken: "tok", Scopes: RequiredScopes()}
}

func TestGrantWithEveryScopeIsUsable(t *testing.T) {
	if err := fullGrant().Usable(time.Now()); err != nil {
		t.Fatalf("usable grant refused: %v", err)
	}
}

func TestGrantMissingAnAdsScopeIsRefused(t *testing.T) {
	g := fullGrant()
	g.Scopes = []string{ScopeAdsRead, ScopePagesShowList}
	err := g.Usable(time.Now())
	if !errors.Is(err, ErrMissingScopes) {
		t.Fatalf("got %v, want ErrMissingScopes", err)
	}
}

func TestRevokedExpiredOrTokenlessGrantNeedsReconnect(t *testing.T) {
	past := time.Now().Add(-time.Minute)
	cases := map[string]func(*Grant){
		"revoked":  func(g *Grant) { g.Status = GrantRevoked },
		"no token": func(g *Grant) { g.AccessToken = "" },
		"expired":  func(g *Grant) { g.TokenExpiresAt = &past },
	}
	for name, mutate := range cases {
		g := fullGrant()
		mutate(g)
		if err := g.Usable(time.Now()); !errors.Is(err, ErrAccountNeedsReconnect) {
			t.Fatalf("%s: got %v", name, err)
		}
	}
	var none *Grant
	if err := none.Usable(time.Now()); !errors.Is(err, ErrAccountNeedsReconnect) {
		t.Fatalf("nil grant: got %v", err)
	}
}

func TestGranularScopesLimitWhichAccountsTheGrantReaches(t *testing.T) {
	g := fullGrant()
	g.GranularScopes = map[string][]string{ScopeAdsManagement: {"111"}}
	if !g.Allows(ScopeAdsManagement, "111") {
		t.Fatal("listed account refused")
	}
	if g.Allows(ScopeAdsManagement, "222") {
		t.Fatal("unlisted account allowed")
	}
	if !g.Allows(ScopeAdsRead, "222") {
		t.Fatal("scope without granular targets should follow meta's own check")
	}
}

func TestScopeNeverGrantedIsNeverAllowed(t *testing.T) {
	g := fullGrant()
	g.Scopes = []string{ScopeAdsRead}
	if g.Allows(ScopeAdsManagement, "111") {
		t.Fatal("ungranted scope allowed")
	}
}
