package oauthstate

import (
	"errors"
	"testing"
)

func newTestIssuer(t *testing.T) *Issuer {
	t.Helper()
	nonces, err := NewNonceStore(newMemoryState(), "fb:oauth")
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := NewIssuer(testSecret, nonces, "/dashboard/facebook-pages")
	if err != nil {
		t.Fatal(err)
	}
	return issuer
}

func TestIssueThenRedeemRoundTrips(t *testing.T) {
	issuer := newTestIssuer(t)
	raw, err := issuer.Issue(IssueInput{WorkspaceID: "ws-1", UserID: "u-1", ReturnPath: "/dashboard/facebook-pages/abc", Popup: true})
	if err != nil {
		t.Fatal(err)
	}
	state, err := issuer.Redeem(raw)
	if err != nil {
		t.Fatalf("redeem: %v", err)
	}
	if state.WorkspaceID != "ws-1" || state.UserID != "u-1" || !state.Popup || issuer.ReturnPath(state) != "/dashboard/facebook-pages/abc" {
		t.Fatalf("state = %+v", state)
	}
}

func TestRedeemIsSingleUse(t *testing.T) {
	issuer := newTestIssuer(t)
	raw, _ := issuer.Issue(IssueInput{WorkspaceID: "ws-1"})
	if _, err := issuer.Redeem(raw); err != nil {
		t.Fatal(err)
	}
	if _, err := issuer.Redeem(raw); !errors.Is(err, ErrReplayedState) {
		t.Fatalf("replay = %v, want ErrReplayedState", err)
	}
}

func TestHostileReturnPathFallsBack(t *testing.T) {
	issuer := newTestIssuer(t)
	raw, _ := issuer.Issue(IssueInput{WorkspaceID: "ws-1", ReturnPath: "https://evil.example"})
	state, _ := issuer.Redeem(raw)
	if got := issuer.ReturnPath(state); got != "/dashboard/facebook-pages" {
		t.Fatalf("return path = %q", got)
	}
}

func TestIssueRequiresWorkspace(t *testing.T) {
	issuer := newTestIssuer(t)
	if _, err := issuer.Issue(IssueInput{}); err == nil {
		t.Fatal("missing workspace accepted")
	}
}

func TestRedeemRejectsForeignSecret(t *testing.T) {
	issuer := newTestIssuer(t)
	other, _ := NewNonceStore(newMemoryState(), "fb:oauth")
	foreign, _ := NewIssuer("another-secret", other, "/x")
	raw, _ := foreign.Issue(IssueInput{WorkspaceID: "ws-1"})
	if _, err := issuer.Redeem(raw); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("got %v, want ErrInvalidState", err)
	}
}

func TestNewIssuerRequiresSecretAndNonces(t *testing.T) {
	nonces, _ := NewNonceStore(newMemoryState(), "fb:oauth")
	if _, err := NewIssuer("", nonces, "/x"); err == nil {
		t.Fatal("empty secret accepted")
	}
	if _, err := NewIssuer("s", nil, "/x"); err == nil {
		t.Fatal("nil nonce store accepted")
	}
}
