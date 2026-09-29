package voipinfra

import (
	"errors"
	"testing"
	"time"

	"github.com/emiago/diago/media"
	"github.com/emiago/sipgo/sip"

	"vozko/domain/sip_trunk"
)

func testDefaults() trunkDefaults {
	return trunkDefaults{
		RegisterExpiry:     25 * time.Second,
		RegisterRetryDelay: 5 * time.Second,
		UnregisterTimeout:  5 * time.Second,
		UserAgent:          "TestTrunk",
		DialTimeout:        60 * time.Second,
		STUNServers:        []string{"stun:default:3478"},
	}
}

func TestRuntimeConfigAppliesDefaults(t *testing.T) {
	trunk := &sip_trunk.SIPTrunk{ID: "t1", Username: "1001"}
	cfg, err := newTrunkRuntimeConfig(trunk, testDefaults())
	if err != nil {
		t.Fatalf("newTrunkRuntimeConfig() error = %v", err)
	}
	if !cfg.RegisterEnabled || cfg.RegisterExpiry != 25*time.Second || cfg.UserAgent != "TestTrunk" {
		t.Fatalf("defaults not applied: %+v", cfg)
	}
	if cfg.AuthUsername != "1001" {
		t.Fatalf("AuthUsername = %q, want 1001", cfg.AuthUsername)
	}
	if cfg.Transport != "udp4" {
		t.Fatalf("Transport = %q, want udp4", cfg.Transport)
	}
	if len(cfg.STUNServers) != 1 || cfg.STUNServers[0] != "stun:default:3478" {
		t.Fatalf("STUNServers = %v, want the defaults", cfg.STUNServers)
	}
	if cfg.Codecs != nil {
		t.Fatalf("Codecs = %v, want nil so diago offers its own defaults", cfg.Codecs)
	}
}

func TestRuntimeConfigHonoursTrunkSettings(t *testing.T) {
	trunk := &sip_trunk.SIPTrunk{
		ID:        "t1",
		Username:  "1001",
		Transport: sip_trunk.TransportTCP,
		Settings: sip_trunk.Settings{
			AuthUsername:          "auth",
			SkipRegistration:      true,
			RegisterExpirySeconds: 120,
			Codecs:                []sip_trunk.Codec{"pcma", sip_trunk.CodecTelephoneEvent},
			SRTPMode:              sip_trunk.SRTPModeRequired,
			STUNServers:           []string{"stun:custom:3478"},
			UserAgent:             "Custom/1.0",
			ExtraInviteHeaders:    map[string]string{"X-B": "2", "X-A": "1"},
		},
	}
	cfg, err := newTrunkRuntimeConfig(trunk, testDefaults())
	if err != nil {
		t.Fatalf("newTrunkRuntimeConfig() error = %v", err)
	}
	if cfg.RegisterEnabled || cfg.RegisterExpiry != 120*time.Second || cfg.AuthUsername != "auth" || cfg.UserAgent != "Custom/1.0" {
		t.Fatalf("settings not applied: %+v", cfg)
	}
	if cfg.Transport != "tcp" {
		t.Fatalf("Transport = %q, want tcp", cfg.Transport)
	}
	if len(cfg.Codecs) != 2 || cfg.Codecs[0] != media.CodecAudioAlaw || cfg.Codecs[1] != media.CodecTelephoneEvent8000 {
		t.Fatalf("Codecs = %v, want PCMA then telephone-event", cfg.Codecs)
	}
	if cfg.STUNServers[0] != "stun:custom:3478" {
		t.Fatalf("STUNServers = %v, want the trunk override", cfg.STUNServers)
	}
	headers := cfg.inviteHeaders("1001", "sip.example.com")
	if len(headers) != 3 || headers[0].Name() != "From" || headers[1].Name() != "X-A" || headers[2].Value() != "2" {
		t.Fatalf("inviteHeaders() = %v, want From then the extra headers sorted by name", headers)
	}
	if from := headers[0].(*sip.FromHeader); from.Address.User != "1001" || from.Address.Host != "sip.example.com" {
		t.Fatalf("From = %v, want the registered AOR 1001@sip.example.com", from)
	}
	if again := cfg.inviteHeaders("1001", "sip.example.com"); again[0] == headers[0] {
		t.Fatal("inviteHeaders() reused header instances across calls")
	}
}

func TestRuntimeConfigRejectsInvalidSettings(t *testing.T) {
	trunk := &sip_trunk.SIPTrunk{ID: "t1", Settings: sip_trunk.Settings{Codecs: []sip_trunk.Codec{"G729"}}}
	if _, err := newTrunkRuntimeConfig(trunk, testDefaults()); !errors.Is(err, sip_trunk.ErrUnsupportedCodec) {
		t.Fatalf("newTrunkRuntimeConfig() error = %v, want ErrUnsupportedCodec", err)
	}
}

func TestRuntimeConfigRejectsUnsupportedTransport(t *testing.T) {
	trunk := &sip_trunk.SIPTrunk{ID: "t1", Transport: "SCTP"}
	if _, err := newTrunkRuntimeConfig(trunk, testDefaults()); !errors.Is(err, sip_trunk.ErrUnsupportedTransport) {
		t.Fatalf("newTrunkRuntimeConfig() error = %v, want ErrUnsupportedTransport", err)
	}
}

func TestRegistrarAddress(t *testing.T) {
	cases := []struct {
		trunk sip_trunk.SIPTrunk
		want  string
	}{
		{sip_trunk.SIPTrunk{Host: "sip.example.com"}, "sip.example.com:5060"},
		{sip_trunk.SIPTrunk{Host: "sip.example.com", Port: 5080}, "sip.example.com:5080"},
	}
	for _, tc := range cases {
		if got := registrarAddress(&tc.trunk); got != tc.want {
			t.Errorf("registrarAddress(%+v) = %q, want %q", tc.trunk, got, tc.want)
		}
	}
}
