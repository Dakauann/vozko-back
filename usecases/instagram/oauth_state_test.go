package instagram

import (
	"errors"
	"strings"
	"testing"
	"time"
)

const testSecret = "instagram-app-secret"

func validState() OAuthState {
	return OAuthState{
		WorkspaceID: "ws-1",
		UserID:      "user-1",
		Nonce:       "nonce-1",
		ExpiresAt:   time.Now().UTC().Add(10 * time.Minute),
		ReturnPath:  "/dashboard/instagram-accounts",
	}
}

func TestEncodeDecodeState_RoundTrip(t *testing.T) {
	want := validState()

	encoded, err := EncodeState(want, testSecret)
	if err != nil {
		t.Fatalf("EncodeState: %v", err)
	}

	got, err := DecodeState(encoded, testSecret)
	if err != nil {
		t.Fatalf("DecodeState: %v", err)
	}
	if got.WorkspaceID != want.WorkspaceID {
		t.Errorf("workspace = %q, want %q", got.WorkspaceID, want.WorkspaceID)
	}
	if got.UserID != want.UserID {
		t.Errorf("user = %q, want %q", got.UserID, want.UserID)
	}
	if got.Nonce != want.Nonce {
		t.Errorf("nonce = %q, want %q", got.Nonce, want.Nonce)
	}
	if got.ReturnPath != want.ReturnPath {
		t.Errorf("returnPath = %q, want %q", got.ReturnPath, want.ReturnPath)
	}
}

// TestDecodeState_RejectsWrongSecret is the CSRF property: a state we did not
// sign must never be accepted, because the callback trusts it to identify the
// tenant.
func TestDecodeState_RejectsWrongSecret(t *testing.T) {
	encoded, err := EncodeState(validState(), testSecret)
	if err != nil {
		t.Fatalf("EncodeState: %v", err)
	}

	if _, err := DecodeState(encoded, "a-different-secret"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("err = %v, want ErrInvalidState", err)
	}
}

// TestDecodeState_RejectsTamperedPayload: flipping the payload must invalidate
// the signature, otherwise a caller could rewrite the workspace id and connect an
// account into someone else's tenant.
func TestDecodeState_RejectsTamperedPayload(t *testing.T) {
	tampered, err := EncodeState(OAuthState{
		WorkspaceID: "attacker-ws",
		UserID:      "user-1",
		Nonce:       "nonce-1",
		ExpiresAt:   time.Now().UTC().Add(10 * time.Minute),
	}, "attacker-secret")
	if err != nil {
		t.Fatalf("EncodeState: %v", err)
	}

	// Keep the attacker's payload, but present it to the real secret.
	if _, err := DecodeState(tampered, testSecret); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("err = %v, want ErrInvalidState", err)
	}

	// Splice a valid signature onto a different payload.
	valid, _ := EncodeState(validState(), testSecret)
	parts := strings.SplitN(valid, ".", 2)
	spliced := strings.SplitN(tampered, ".", 2)[0] + "." + parts[1]
	if _, err := DecodeState(spliced, testSecret); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("spliced signature accepted: err = %v", err)
	}
}

func TestDecodeState_RejectsExpired(t *testing.T) {
	expired := validState()
	expired.ExpiresAt = time.Now().UTC().Add(-time.Minute)

	encoded, err := EncodeState(expired, testSecret)
	if err != nil {
		t.Fatalf("EncodeState: %v", err)
	}
	if _, err := DecodeState(encoded, testSecret); !errors.Is(err, ErrExpiredState) {
		t.Fatalf("err = %v, want ErrExpiredState", err)
	}
}

func TestDecodeState_RejectsMalformed(t *testing.T) {
	for _, raw := range []string{"", ".", "nodot", "abc.", ".abc", "not-base64!!.deadbeef"} {
		if _, err := DecodeState(raw, testSecret); err == nil {
			t.Errorf("DecodeState(%q) = nil error, want failure", raw)
		}
	}
}

func TestEncodeState_RejectsDelimiterInFields(t *testing.T) {
	bad := validState()
	bad.WorkspaceID = "ws|injected"

	if _, err := EncodeState(bad, testSecret); err == nil {
		t.Fatal("EncodeState accepted a field containing the payload delimiter")
	}
}

func TestDecodeState_RequiresWorkspaceAndNonce(t *testing.T) {
	noWorkspace := validState()
	noWorkspace.WorkspaceID = ""
	encoded, _ := EncodeState(noWorkspace, testSecret)
	if _, err := DecodeState(encoded, testSecret); !errors.Is(err, ErrInvalidState) {
		t.Errorf("empty workspace accepted: %v", err)
	}

	noNonce := validState()
	noNonce.Nonce = ""
	encoded, _ = EncodeState(noNonce, testSecret)
	if _, err := DecodeState(encoded, testSecret); !errors.Is(err, ErrInvalidState) {
		t.Errorf("empty nonce accepted: %v", err)
	}
}

func TestNewNonce_IsRandomAndNonEmpty(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 64; i++ {
		nonce, err := NewNonce()
		if err != nil {
			t.Fatalf("NewNonce: %v", err)
		}
		if nonce == "" {
			t.Fatal("NewNonce returned an empty string")
		}
		if seen[nonce] {
			t.Fatalf("NewNonce repeated a value: %q", nonce)
		}
		seen[nonce] = true
	}
}

// TestSafeReturnPath_BlocksOpenRedirects: the return path travels inside the
// signed state, so a tenant admin (or anyone who can craft a start request) must
// not be able to turn the callback into a redirect to another origin.
func TestSafeReturnPath_BlocksOpenRedirects(t *testing.T) {
	const fallback = "/dashboard/instagram-accounts"

	hostile := []string{
		"https://evil.example/steal",
		"//evil.example/steal",
		"http://evil.example",
		"/\\evil.example",
		"javascript://evil",
		"dashboard/no-leading-slash",
		"",
		"   ",
	}
	for _, candidate := range hostile {
		if got := SafeReturnPath(candidate, fallback); got != fallback {
			t.Errorf("SafeReturnPath(%q) = %q, want the fallback", candidate, got)
		}
	}

	safe := []string{
		"/dashboard/instagram-accounts",
		"/dashboard/instagram-accounts/abc",
		"/dashboard/live-chat?tab=1",
	}
	for _, candidate := range safe {
		if got := SafeReturnPath(candidate, fallback); got != candidate {
			t.Errorf("SafeReturnPath(%q) = %q, want it preserved", candidate, got)
		}
	}
}
