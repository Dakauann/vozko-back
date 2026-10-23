package carriermessaging_usecase

import (
	"errors"
	"strings"
	"testing"

	"vozko/domain/rcs"
)

const linkSecret = "0123456789abcdef0123456789abcdef"

func TestLinkTokensRoundTrip(t *testing.T) {
	links, err := NewLinkTokens(linkSecret, "https://api.example.com/")
	if err != nil {
		t.Fatalf("NewLinkTokens = %v", err)
	}
	key := rcs.ButtonKey{Card: 2, Index: 1}
	url, err := links.TrackedURL("0b7d2a3e-5f4c-4e8b-9a1d-2c3b4a5d6e7f", key)
	if err != nil || !strings.HasPrefix(url, "https://api.example.com/public/rcs/r/") {
		t.Fatalf("TrackedURL = %q, %v", url, err)
	}
	token := strings.TrimPrefix(url, "https://api.example.com/public/rcs/r/")
	id, got, err := links.Verify(token)
	if err != nil || id != "0b7d2a3e-5f4c-4e8b-9a1d-2c3b4a5d6e7f" || got != key {
		t.Fatalf("Verify = %q, %+v, %v", id, got, err)
	}
}

func TestLinkTokensRefuseTampering(t *testing.T) {
	links, _ := NewLinkTokens(linkSecret, "https://api.example.com")
	url, _ := links.TrackedURL("msg-1", rcs.ButtonKey{Card: -1, Index: 0})
	token := strings.TrimPrefix(url, "https://api.example.com/public/rcs/r/")
	other, _ := NewLinkTokens(strings.Repeat("z", 32), "https://api.example.com")
	for _, bad := range []string{
		"", "abc", strings.Replace(token, "m0", "m1", 1), token + "0", strings.Replace(token, "msg-1", "msg-2", 1),
	} {
		if _, _, err := links.Verify(bad); !errors.Is(err, ErrLinkInvalid) {
			t.Fatalf("Verify(%q) = %v", bad, err)
		}
	}
	if _, _, err := other.Verify(token); !errors.Is(err, ErrLinkInvalid) {
		t.Fatal("a token signed with another secret must be refused")
	}
}

func TestLinkTokensNeedAStrongSecretAndAnHTTPSBase(t *testing.T) {
	if _, err := NewLinkTokens("short", "https://api.example.com"); err == nil {
		t.Fatal("short secret accepted")
	}
	if _, err := NewLinkTokens(linkSecret, "http://api.example.com"); err == nil {
		t.Fatal("plain http base accepted")
	}
}
