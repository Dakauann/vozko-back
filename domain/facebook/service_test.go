package facebook

import (
	"errors"
	"testing"
)

func TestSubscribedFieldsAreAllValid(t *testing.T) {
	if bad := InvalidSubscribedFields(SubscribedFields()); len(bad) != 0 {
		t.Fatalf("invalid default fields: %v", bad)
	}
	if bad := InvalidSubscribedFields([]string{"feed", "message_edit"}); len(bad) != 1 || bad[0] != "message_edit" {
		t.Fatalf("the instagram spelling must be rejected for pages: %v", bad)
	}
}

func TestValidateRedirectURI(t *testing.T) {
	ok := []string{"https://api.example.com/oauth/facebook/callback", "http://localhost:8080/oauth/facebook/callback/"}
	for _, raw := range ok {
		if err := ValidateRedirectURI(raw); err != nil {
			t.Errorf("%s: %v", raw, err)
		}
	}
	bad := []string{"", "api.example.com/oauth/facebook/callback", "http://api.example.com/oauth/facebook/callback",
		"https://api.example.com/oauth/instagram/callback", "https://api.example.com/oauth/facebook/callback?x=1"}
	for _, raw := range bad {
		if err := ValidateRedirectURI(raw); err == nil {
			t.Errorf("%s accepted", raw)
		}
	}
}

func TestRedirectURIForAnotherCallbackKeepsTheSameRules(t *testing.T) {
	if err := ValidateRedirectURIFor("https://api.example.com/oauth/meta-ads/callback", "/oauth/meta-ads/callback"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRedirectURIFor("https://api.example.com/oauth/facebook/callback", "/oauth/meta-ads/callback"); err == nil {
		t.Fatal("pages callback accepted for the ads flow")
	}
	if err := ValidateRedirectURIFor("http://api.example.com/oauth/meta-ads/callback", "/oauth/meta-ads/callback"); err == nil {
		t.Fatal("plain http accepted")
	}
}

func TestRequiredScopesCoverEveryCapability(t *testing.T) {
	p := &Page{Status: StatusConnected, GrantedScopes: RequiredScopes(), Tasks: []Task{TaskManage}}
	for _, c := range AllCapabilities() {
		if !p.Can(c) {
			t.Errorf("the requested scope set cannot %s", c)
		}
	}
}

func TestPagesRestrictedByTheGrantStayRestricted(t *testing.T) {
	d := &TokenDebug{Scopes: []string{ScopeShowList}, GranularScopes: map[string][]string{ScopeShowList: {"111"}}}
	if err := d.AdoptListedPages([]string{"111", "222"}); err != nil {
		t.Fatal(err)
	}
	if got := d.GranularScopes[ScopeShowList]; len(got) != 1 || got[0] != "111" {
		t.Fatalf("targets = %v", got)
	}
}

func TestAnUnrestrictedGrantCoversThePagesTheTokenCanList(t *testing.T) {
	d := &TokenDebug{Scopes: []string{ScopeShowList, ScopeMessaging}, GranularScopes: map[string][]string{ScopeShowList: nil, ScopeMessaging: nil}}
	if err := d.AdoptListedPages([]string{"111", "222"}); err != nil {
		t.Fatal(err)
	}
	if got := d.GranularScopes[ScopeShowList]; len(got) != 2 {
		t.Fatalf("targets = %v", got)
	}
}

func TestAGrantWithoutThePageListPermissionIsRefused(t *testing.T) {
	d := &TokenDebug{Scopes: []string{ScopeMessaging}}
	if err := d.AdoptListedPages([]string{"111"}); !errors.Is(err, ErrGrantUnverifiable) {
		t.Fatalf("got %v", err)
	}
}

func TestTokenKindComesFromTheDebugType(t *testing.T) {
	for raw, want := range map[string]TokenKind{"SYSTEM_USER": TokenSystemUser, "system_user": TokenSystemUser, "USER": TokenUser, "": TokenUser} {
		if got := (&TokenDebug{Type: raw}).Kind(); got != want {
			t.Errorf("%q -> %s, want %s", raw, got, want)
		}
	}
}
