package sip_trunk

import (
	"errors"
	"strings"
	"testing"
)

func newValidTrunk() *SIPTrunk {
	return &SIPTrunk{
		ID:           "11111111-1111-1111-1111-111111111111",
		WorkspaceID:  "ws-1",
		Name:         "Acme Trunk",
		Host:         "sip.acme.com",
		Port:         5060,
		Username:     "alice",
		Password:     "s3cret",
		Transport:    TransportUDP,
		TrunkType:    TrunkTypeMobile,
		IsRotational: true,
	}
}

func TestValidate_Host_BlocksSSRFTargets(t *testing.T) {
	cases := []string{
		"127.0.0.1",
		"localhost",
		"LOCALHOST",
		"169.254.169.254",
		"10.0.0.1",
		"192.168.1.1",
		"172.16.0.1",
		"::1",
		"fe80::1",
		"0.0.0.0",
		"224.0.0.1",
	}
	for _, host := range cases {
		t.Run(host, func(t *testing.T) {
			tr := newValidTrunk()
			tr.Host = host
			if err := tr.Validate(); err == nil {
				t.Fatalf("expected error for host=%q, got nil", host)
			} else if !errors.Is(err, ErrTrunkHostBlocked) && !errors.Is(err, ErrTrunkHostInvalid) {
				t.Fatalf("expected ErrTrunkHostBlocked/Invalid for %q, got %v", host, err)
			}
		})
	}
}

func TestValidate_Username_RejectsControlChars(t *testing.T) {
	tr := newValidTrunk()
	tr.Username = "alice\r\nFrom: <sip:victim@evil>"
	if err := tr.Validate(); !errors.Is(err, ErrTrunkUsernameInvalid) {
		t.Fatalf("want ErrTrunkUsernameInvalid, got %v", err)
	}
}

func TestValidate_Password_RejectsControlChars(t *testing.T) {
	for _, bad := range []string{"pass\r\nWWW-Authenticate: x", "pass\x00word", "pass\tword"} {
		tr := newValidTrunk()
		tr.Password = bad
		if err := tr.Validate(); !errors.Is(err, ErrTrunkPasswordInvalid) {
			t.Fatalf("want ErrTrunkPasswordInvalid for %q, got %v", bad, err)
		}
	}
}

func TestValidateAdvanced_RejectsCRLFOnIdentityFields(t *testing.T) {
	bad := "value\r\nX-Injected: pwned"
	fields := map[string]func(*SIPTrunk){
		"UserAgent":     func(s *SIPTrunk) { v := bad; s.UserAgent = &v },
		"AuthUsername":  func(s *SIPTrunk) { v := bad; s.AuthUsername = &v },
		"OutboundProxy": func(s *SIPTrunk) { v := bad; s.OutboundProxy = &v },
		"DialPrefix":    func(s *SIPTrunk) { v := bad; s.DialPrefix = &v },
		"PublicAddress": func(s *SIPTrunk) { v := bad; s.PublicAddress = &v },
	}
	for name, mutate := range fields {
		t.Run(name, func(t *testing.T) {
			tr := newValidTrunk()
			mutate(tr)
			if err := tr.Validate(); err == nil {
				t.Fatalf("%s: expected validation error for CRLF payload, got nil", name)
			}
		})
	}
}

func TestValidateAdvanced_RejectsTabAndNullBytes(t *testing.T) {
	tr := newValidTrunk()
	v := "ok\x00still"
	tr.UserAgent = &v
	if err := tr.Validate(); !errors.Is(err, ErrTrunkIdentityFieldInvalid) {
		t.Fatalf("want ErrTrunkIdentityFieldInvalid for NUL byte, got %v", err)
	}

	tr2 := newValidTrunk()
	v2 := "value\twith\ttab"
	tr2.AuthUsername = &v2
	if err := tr2.Validate(); !errors.Is(err, ErrTrunkIdentityFieldInvalid) {
		t.Fatalf("want ErrTrunkIdentityFieldInvalid for TAB, got %v", err)
	}
}

func TestValidateAdvanced_LengthCapsOnIdentityFields(t *testing.T) {
	long129 := strings.Repeat("a", 129)

	tr := newValidTrunk()
	tr.UserAgent = &long129
	if err := tr.Validate(); !errors.Is(err, ErrTrunkUserAgentInvalid) {
		t.Fatalf("UserAgent len 129 must fail: %v", err)
	}

	tr2 := newValidTrunk()
	tr2.AuthUsername = &long129
	if err := tr2.Validate(); !errors.Is(err, ErrTrunkIdentityFieldInvalid) {
		t.Fatalf("AuthUsername len 129 must fail: %v", err)
	}
}

func TestValidateAdvanced_StunServer_BlocksLocalAndPrivate(t *testing.T) {
	cases := []string{
		"127.0.0.1:3478",
		"169.254.169.254:3478",
		"10.0.0.1:3478",
		"192.168.0.1:3478",
		"localhost:3478",
		"[::1]:3478",
	}
	for _, s := range cases {
		t.Run(s, func(t *testing.T) {
			tr := newValidTrunk()
			tr.StunServers = []string{s}
			if err := tr.Validate(); !errors.Is(err, ErrTrunkStunServerBlocked) {
				t.Fatalf("want ErrTrunkStunServerBlocked for %q, got %v", s, err)
			}
		})
	}
}

func TestValidateAdvanced_StunServer_MalformedRejected(t *testing.T) {
	tr := newValidTrunk()
	tr.StunServers = []string{"no-port"}
	if err := tr.Validate(); !errors.Is(err, ErrTrunkStunServerInvalid) {
		t.Fatalf("want ErrTrunkStunServerInvalid, got %v", err)
	}
}

func TestValidateAdvanced_StunServer_PublicAllowed(t *testing.T) {
	tr := newValidTrunk()
	tr.StunServers = []string{"stun.l.google.com:19302"}
	if err := tr.Validate(); err != nil {
		t.Fatalf("public stun should be accepted, got %v", err)
	}
}

func TestValidateAdvanced_StunServer_ArrayCap(t *testing.T) {
	tr := newValidTrunk()
	tr.StunServers = make([]string, maxStunServers+1)
	for i := range tr.StunServers {
		tr.StunServers[i] = "stun" + string(rune('a'+i)) + ".example.com:3478"
	}
	if err := tr.Validate(); !errors.Is(err, ErrTrunkTooManyEntries) {
		t.Fatalf("want ErrTrunkTooManyEntries, got %v", err)
	}
}

func TestValidateAdvanced_OutboundProxy_BlocksLocal(t *testing.T) {
	tr := newValidTrunk()
	v := "127.0.0.1:5060"
	tr.OutboundProxy = &v
	if err := tr.Validate(); !errors.Is(err, ErrTrunkOutboundProxyInvalid) {
		t.Fatalf("want ErrTrunkOutboundProxyInvalid, got %v", err)
	}
}

func TestValidateAdvanced_PublicAddress_AcceptsHostsAndIPs(t *testing.T) {
	tr := newValidTrunk()
	tr.PublicAddress = strPtr("203.0.113.10")
	if err := tr.Validate(); err != nil {
		t.Fatalf("public IPv4 should be accepted, got %v", err)
	}

	tr.PublicAddress = strPtr("2001:db8::1")
	if err := tr.Validate(); err != nil {
		t.Fatalf("public IPv6 should be accepted, got %v", err)
	}

	tr.PublicAddress = strPtr("gateway.example.com")
	if err := tr.Validate(); err != nil {
		t.Fatalf("hostname should be accepted (no mode gating), got %v", err)
	}
}

func TestIsOwnedBy_RejectsEmptyAndSystemTrunks(t *testing.T) {
	tr := newValidTrunk()
	tr.WorkspaceID = ""
	if tr.IsOwnedBy("") {
		t.Fatalf("system trunk must not match empty workspace id")
	}
	if tr.IsOwnedBy("ws-1") {
		t.Fatalf("system trunk must not be owned by any workspace")
	}

	tr.WorkspaceID = "ws-1"
	if tr.IsOwnedBy("") {
		t.Fatalf("workspace trunk must not match empty workspace id")
	}
	if !tr.IsOwnedBy("ws-1") {
		t.Fatalf("expected owner to match")
	}
	if tr.IsOwnedBy("ws-2") {
		t.Fatalf("cross-workspace match must be rejected")
	}
}

func TestCanAcceptInbound(t *testing.T) {
	tr := newValidTrunk()
	tr.WorkspaceID = "ws-1"

	if err := tr.CanAcceptInbound("ws-1"); err != nil {
		t.Fatalf("owned non-global trunk should accept inbound: %v", err)
	}

	if err := tr.CanAcceptInbound("ws-other"); !errors.Is(err, ErrInboundNotOwned) {
		t.Fatalf("non-owned trunk should reject with ErrInboundNotOwned, got %v", err)
	}

	tr.IsGloballyVisible = true
	if err := tr.CanAcceptInbound("ws-1"); !errors.Is(err, ErrInboundGlobalTrunk) {
		t.Fatalf("global trunk should reject with ErrInboundGlobalTrunk, got %v", err)
	}

	emptyTrunk := &SIPTrunk{WorkspaceID: ""}
	if err := emptyTrunk.CanAcceptInbound("ws-1"); !errors.Is(err, ErrInboundNotOwned) {
		t.Fatalf("system trunk should reject with ErrInboundNotOwned, got %v", err)
	}
}

func strPtr(s string) *string { return &s }
