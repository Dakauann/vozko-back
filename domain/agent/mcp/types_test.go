package mcp

import (
	"testing"
	"time"
)

func TestAuthModeValid(t *testing.T) {
	for _, m := range []AuthMode{AuthNone, AuthAPIKey, AuthOAuth2} {
		if !m.Valid() {
			t.Fatalf("%s should be valid", m)
		}
	}
	if AuthMode("other").Valid() {
		t.Fatal("unknown auth mode must be invalid")
	}
}

func TestWorkspaceID(t *testing.T) {
	var w WorkspaceID
	if !w.Empty() {
		t.Fatal("empty workspace")
	}
	w = "abc"
	if w.Empty() {
		t.Fatal("non-empty workspace")
	}
	if w.String() != "abc" {
		t.Fatalf("got %q", w.String())
	}
}

func TestNowIsReplaceable(t *testing.T) {
	orig := Now
	t.Cleanup(func() { Now = orig })
	fixed := time.Unix(1700000000, 0)
	Now = func() time.Time { return fixed }
	if !Now().Equal(fixed) {
		t.Fatal("Now seam failed")
	}
}
