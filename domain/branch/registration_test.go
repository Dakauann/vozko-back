package branch

import (
	"testing"
	"time"
)

// TestVerifyDigest_RFC2617Vector uses the canonical RFC 2617 section 3.5 example
// (qop=auth) to prove the HA1-based response derivation is correct.
func TestVerifyDigest_RFC2617Vector(t *testing.T) {
	ha1 := ComputeHA1("Mufasa", "testrealm@host.com", "Circle Of Life") // 939e7578ed9e3c518a452acee763bce9
	const (
		method   = "GET"
		uri      = "/dir/index.html"
		nonce    = "dcd98b7102dd2f0e8b11d0f600bfb0c093"
		cnonce   = "0a4f113b"
		nc       = "00000001"
		qop      = "auth"
		expected = "6629fae49393a05397450978507c4ef1"
	)
	if got := ComputeDigestResponse(ha1, method, uri, nonce, qop, nc, cnonce); got != expected {
		t.Fatalf("ComputeDigestResponse = %q, want RFC 2617 %q", got, expected)
	}
	if !VerifyDigest(ha1, method, uri, nonce, qop, nc, cnonce, expected) {
		t.Fatal("VerifyDigest rejected the RFC 2617 vector")
	}
	if VerifyDigest(ha1, method, uri, nonce, qop, nc, cnonce, "deadbeef") {
		t.Fatal("VerifyDigest accepted a wrong response")
	}
	if VerifyDigest("", method, uri, nonce, qop, nc, cnonce, expected) {
		t.Fatal("VerifyDigest accepted an empty HA1")
	}
}

// TestVerifyDigest_NoQopRoundTrip covers the legacy (RFC 2069) no-qop path.
func TestVerifyDigest_NoQopRoundTrip(t *testing.T) {
	ha1 := ComputeHA1("1001", "vozko", "s3cret")
	resp := ComputeDigestResponse(ha1, "REGISTER", "sip:vozko", "nonce-xyz", "", "", "")
	if !VerifyDigest(ha1, "REGISTER", "sip:vozko", "nonce-xyz", "", "", "", resp) {
		t.Fatal("no-qop digest did not round-trip")
	}
	// A different nonce must not verify against the same response.
	if VerifyDigest(ha1, "REGISTER", "sip:vozko", "other-nonce", "", "", "", resp) {
		t.Fatal("digest verified under a different nonce")
	}
}

func TestClampExpires(t *testing.T) {
	cases := []struct {
		in          int
		wantGranted int
		wantBrief   bool
	}{
		{in: -1, wantGranted: DefaultExpiresSeconds},
		{in: 0, wantGranted: 0},
		{in: 5, wantGranted: MinExpiresSeconds, wantBrief: true},
		{in: 60, wantGranted: 60},
		{in: 99999, wantGranted: MaxExpiresSeconds},
	}
	for _, c := range cases {
		g, brief := ClampExpires(c.in)
		if g != c.wantGranted || brief != c.wantBrief {
			t.Errorf("ClampExpires(%d) = (%d,%v), want (%d,%v)", c.in, g, brief, c.wantGranted, c.wantBrief)
		}
	}
}

func TestBindingExpired(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	live := RegistrationBinding{ExpiresAt: now.Add(time.Second)}
	dead := RegistrationBinding{ExpiresAt: now.Add(-time.Second)}
	if live.Expired(now) {
		t.Error("future binding reported expired")
	}
	if !dead.Expired(now) {
		t.Error("past binding reported live")
	}
	if !live.Expired(live.ExpiresAt) {
		t.Error("binding at exactly its expiry should be expired")
	}
}
