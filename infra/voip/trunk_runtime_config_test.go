package voipinfra

import (
	"strings"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"

	"vozko/domain/sip_trunk"
)

func testDefaults() trunkDefaults {
	return trunkDefaults{
		RegisterExpiry:     60 * time.Second,
		RegisterRetryDelay: 5 * time.Second,
		UnregisterTimeout:  3 * time.Second,
		UserAgentFormat:    "VozkoTrunk/%s",
		DialTimeout:        30 * time.Second,
		MediaWait:          15 * time.Second,
		STUNTimeout:        2 * time.Second,
		PublicIPTimeout:    2 * time.Second,
		STUNServers:        nil,
	}
}

func baseTrunk() *sip_trunk.SIPTrunk {
	return &sip_trunk.SIPTrunk{
		ID:        "trunk-1",
		Host:      "sip.example.com",
		Port:      5060,
		Username:  "alice",
		Password:  "s3cret",
		Domain:    "sip.example.com",
		Transport: sip_trunk.TransportUDP,
		TrunkType: sip_trunk.TrunkTypeMobile,
	}
}

func findHeader(hs []sip.Header, name string) (string, bool) {
	for _, h := range hs {
		if strings.EqualFold(h.Name(), name) {
			return h.Value(), true
		}
	}
	return "", false
}

func TestNewTrunkRuntimeConfig_DefaultsPreserved(t *testing.T) {
	cfg := newTrunkRuntimeConfig(baseTrunk(), testDefaults(), nil)

	if cfg.UserAgent != "VozkoTrunk/trunk-1" {
		t.Fatalf("default UA: got %q", cfg.UserAgent)
	}
	if !cfg.RegisterEnabled {
		t.Fatal("RegisterEnabled must default to true")
	}
	if cfg.RegisterExpiry != 60*time.Second {
		t.Fatalf("RegisterExpiry: got %v", cfg.RegisterExpiry)
	}
	if cfg.RegisterRetryDelay != 5*time.Second {
		t.Fatalf("RegisterRetryDelay: got %v", cfg.RegisterRetryDelay)
	}
	if cfg.AuthUsername != "alice" {
		t.Fatalf("AuthUsername default: got %q", cfg.AuthUsername)
	}
	if cfg.OutboundProxy != "" {
		t.Fatalf("OutboundProxy must default to empty: got %q", cfg.OutboundProxy)
	}
	if cfg.BindHostOverride != "" || cfg.BindPortOverride != 0 {
		t.Fatalf("bind defaults non-zero: host=%q port=%d", cfg.BindHostOverride, cfg.BindPortOverride)
	}
	if !cfg.STUNEnabled {
		t.Fatal("STUNEnabled default true")
	}
	if cfg.SRTPMode != sip_trunk.SRTPModeDisabled {
		t.Fatalf("SRTPMode default: got %v", cfg.SRTPMode)
	}
	if cfg.NumberFormat != sip_trunk.NumberFormatPassthrough {
		t.Fatalf("NumberFormat default: got %v", cfg.NumberFormat)
	}
	if cfg.DialTimeout != 30*time.Second {
		t.Fatalf("DialTimeout default: got %v", cfg.DialTimeout)
	}
	if len(cfg.AllowRegHeaders) == 0 {
		t.Fatal("AllowRegHeaders must include the canonical method list")
	}
	if len(cfg.ExtraInviteHeaders) != 0 {
		t.Fatalf("no extra INVITE headers expected by default, got %d", len(cfg.ExtraInviteHeaders))
	}

	if len(cfg.Codecs) < 2 {
		t.Fatalf("default codec list too small: %d", len(cfg.Codecs))
	}
}

func TestNewTrunkRuntimeConfig_UserAgentOverride(t *testing.T) {
	tr := baseTrunk()
	ua := "MyClient/1.0"
	tr.UserAgent = &ua

	cfg := newTrunkRuntimeConfig(tr, testDefaults(), nil)
	if cfg.UserAgent != "MyClient/1.0" {
		t.Fatalf("UA override not applied: got %q", cfg.UserAgent)
	}
}

func TestNewTrunkRuntimeConfig_AuthUsernameOverride(t *testing.T) {
	tr := baseTrunk()
	auth := "alice-auth"
	tr.AuthUsername = &auth

	cfg := newTrunkRuntimeConfig(tr, testDefaults(), nil)
	if cfg.AuthUsername != "alice-auth" {
		t.Fatalf("AuthUsername override not applied: got %q", cfg.AuthUsername)
	}
}

func TestNewTrunkRuntimeConfig_RegisterEnabledFalse(t *testing.T) {
	tr := baseTrunk()
	off := false
	tr.RegisterEnabled = &off

	cfg := newTrunkRuntimeConfig(tr, testDefaults(), nil)
	if cfg.RegisterEnabled {
		t.Fatal("RegisterEnabled must be honoured when set to false")
	}
}

func TestNewTrunkRuntimeConfig_RegisterTimers(t *testing.T) {
	tr := baseTrunk()
	exp := 3600
	retry := 30
	tr.RegisterExpirySeconds = &exp
	tr.RegisterRetrySeconds = &retry

	cfg := newTrunkRuntimeConfig(tr, testDefaults(), nil)
	if cfg.RegisterExpiry != time.Hour {
		t.Fatalf("RegisterExpiry: got %v", cfg.RegisterExpiry)
	}
	if cfg.RegisterRetryDelay != 30*time.Second {
		t.Fatalf("RegisterRetryDelay: got %v", cfg.RegisterRetryDelay)
	}
}

func TestNewTrunkRuntimeConfig_OutboundProxyOverride(t *testing.T) {
	tr := baseTrunk()
	pr := "sbc.example.com:5060"
	tr.OutboundProxy = &pr

	cfg := newTrunkRuntimeConfig(tr, testDefaults(), nil)
	if cfg.OutboundProxy != "sbc.example.com:5060" {
		t.Fatalf("OutboundProxy: got %q", cfg.OutboundProxy)
	}
}

func TestNewTrunkRuntimeConfig_BindHostAndPortOverride(t *testing.T) {
	tr := baseTrunk()
	host := "10.0.0.5"
	port := 5070
	tr.BindHost = &host
	tr.BindPort = &port

	cfg := newTrunkRuntimeConfig(tr, testDefaults(), nil)
	if cfg.BindHostOverride != "10.0.0.5" || cfg.BindPortOverride != 5070 {
		t.Fatalf("bind override: host=%q port=%d", cfg.BindHostOverride, cfg.BindPortOverride)
	}
}

func TestNewTrunkRuntimeConfig_PublicAddressOverride(t *testing.T) {
	tr := baseTrunk()
	ip := "203.0.113.10"
	tr.PublicAddress = &ip

	cfg := newTrunkRuntimeConfig(tr, testDefaults(), nil)
	if cfg.PublicAddress != "203.0.113.10" {
		t.Fatalf("PublicAddress: got %q", cfg.PublicAddress)
	}
}

func TestNewTrunkRuntimeConfig_StunFlagsAndServers(t *testing.T) {
	tr := baseTrunk()
	off := false
	tr.StunEnabled = &off
	tr.StunServers = []string{"stun.l.google.com:19302"}

	cfg := newTrunkRuntimeConfig(tr, testDefaults(), nil)
	if cfg.STUNEnabled {
		t.Fatal("STUNEnabled must be honoured when set to false")
	}
	if len(cfg.STUNServers) != 1 || cfg.STUNServers[0] != "stun.l.google.com:19302" {
		t.Fatalf("STUNServers: %#v", cfg.STUNServers)
	}
}

func TestResolveCodecList_PerTrunkOverrideAndPTime(t *testing.T) {
	tr := baseTrunk()
	tr.Codecs = []sip_trunk.CodecID{sip_trunk.CodecIDPCMU, sip_trunk.CodecIDPCMA}
	ptime := 20
	tr.PTimeMs = &ptime
	pt := 101
	tr.DTMFPayloadType = &pt

	cfg := newTrunkRuntimeConfig(tr, testDefaults(), nil)

	if len(cfg.Codecs) != 3 {
		t.Fatalf("expected 2 voice codecs + telephone-event, got %d", len(cfg.Codecs))
	}
	if cfg.Codecs[0].Name != CodecPCMU.Name {
		t.Fatalf("ordering wrong: first codec is %q", cfg.Codecs[0].Name)
	}
	if cfg.Codecs[0].SampleDur != 20*time.Millisecond {
		t.Fatalf("PTime not applied: got %v", cfg.Codecs[0].SampleDur)
	}

	last := cfg.Codecs[len(cfg.Codecs)-1]
	if last.Name != CodecTelephoneEvent.Name {
		t.Fatalf("telephone-event missing or out of order: %q", last.Name)
	}
	if last.PayloadType != 101 {
		t.Fatalf("DTMF payload type override not applied: %d", last.PayloadType)
	}
}

func TestNewTrunkRuntimeConfig_SRTPModeOverride(t *testing.T) {
	tr := baseTrunk()
	tr.Transport = sip_trunk.TransportTLS
	mode := sip_trunk.SRTPModeRequired
	tr.SRTPMode = &mode

	cfg := newTrunkRuntimeConfig(tr, testDefaults(), nil)
	if cfg.SRTPMode != sip_trunk.SRTPModeRequired {
		t.Fatalf("SRTPMode override: got %v", cfg.SRTPMode)
	}
}

func TestApplyDialPlan_StripPrefixAndFormat(t *testing.T) {
	cfg := trunkRuntimeConfig{
		DialPrefix:      "9",
		DialStripDigits: 2,
		NumberFormat:    sip_trunk.NumberFormatE164,
	}
	got := cfg.ApplyDialPlan("00551199998888")

	if got != "9+551199998888" {
		t.Fatalf("ApplyDialPlan: got %q", got)
	}
}

func TestApplyDialPlan_NationalDropsPlus(t *testing.T) {
	cfg := trunkRuntimeConfig{NumberFormat: sip_trunk.NumberFormatNational}
	if got := cfg.ApplyDialPlan("+551199998888"); got != "551199998888" {
		t.Fatalf("National format: got %q", got)
	}
}

func TestApplyDialPlan_PassthroughEmpty(t *testing.T) {
	cfg := trunkRuntimeConfig{NumberFormat: sip_trunk.NumberFormatPassthrough}
	if got := cfg.ApplyDialPlan(""); got != "" {
		t.Fatalf("empty input must return empty, got %q", got)
	}
	if got := cfg.ApplyDialPlan("5511"); got != "5511" {
		t.Fatalf("passthrough must not mutate, got %q", got)
	}
}

func TestBuildInviteHeaders_HideCallerIDEmitsPrivacy(t *testing.T) {
	tr := baseTrunk()
	hide := true
	tr.HideCallerID = &hide

	cfg := newTrunkRuntimeConfig(tr, testDefaults(), nil)
	v, ok := findHeader(cfg.ExtraInviteHeaders, "Privacy")
	if !ok || v != "id" {
		t.Fatalf("Privacy header missing or wrong: ok=%v v=%q", ok, v)
	}
}

func TestBuildInviteHeaders_SessionTimerWithRefresher(t *testing.T) {
	tr := baseTrunk()
	on := true
	exp := 1200
	minSE := 120
	ref := sip_trunk.SessionRefresherUAC
	tr.SessionTimerEnabled = &on
	tr.SessionTimerSeconds = &exp
	tr.MinSESeconds = &minSE
	tr.SessionRefresher = &ref

	cfg := newTrunkRuntimeConfig(tr, testDefaults(), nil)

	se, ok := findHeader(cfg.ExtraInviteHeaders, "Session-Expires")
	if !ok || se != "1200;refresher=uac" {
		t.Fatalf("Session-Expires: ok=%v v=%q", ok, se)
	}
	sup, ok := findHeader(cfg.ExtraInviteHeaders, "Supported")
	if !ok || sup != "timer" {
		t.Fatalf("Supported: ok=%v v=%q", ok, sup)
	}
	mse, ok := findHeader(cfg.ExtraInviteHeaders, "Min-SE")
	if !ok || mse != "120" {
		t.Fatalf("Min-SE: ok=%v v=%q", ok, mse)
	}
}

func TestBuildInviteHeaders_SessionTimerDefaults(t *testing.T) {
	tr := baseTrunk()
	on := true
	tr.SessionTimerEnabled = &on

	cfg := newTrunkRuntimeConfig(tr, testDefaults(), nil)

	se, _ := findHeader(cfg.ExtraInviteHeaders, "Session-Expires")
	if se != "1800" {
		t.Fatalf("Session-Expires default: got %q", se)
	}
	mse, _ := findHeader(cfg.ExtraInviteHeaders, "Min-SE")
	if mse != "90" {
		t.Fatalf("Min-SE default: got %q", mse)
	}
}

func TestBuildInviteHeaders_NoTimerWhenDisabled(t *testing.T) {
	cfg := newTrunkRuntimeConfig(baseTrunk(), testDefaults(), nil)
	if _, ok := findHeader(cfg.ExtraInviteHeaders, "Session-Expires"); ok {
		t.Fatal("Session-Expires must NOT be emitted when timer disabled")
	}
}

func TestNewTrunkRuntimeConfig_RingingTimeoutDrivesDialTimeout(t *testing.T) {
	tr := baseTrunk()
	to := 12
	tr.RingingTimeoutSeconds = &to

	cfg := newTrunkRuntimeConfig(tr, testDefaults(), nil)
	if cfg.DialTimeout != 12*time.Second {
		t.Fatalf("DialTimeout: got %v", cfg.DialTimeout)
	}
}
