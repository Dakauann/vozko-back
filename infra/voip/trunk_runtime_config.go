package voipinfra

import (
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"github.com/emiago/diago/media"
	"github.com/emiago/sipgo/sip"

	"vozko/domain/sip_trunk"
)

type trunkDefaults struct {
	RegisterExpiry     time.Duration
	RegisterRetryDelay time.Duration
	UnregisterTimeout  time.Duration
	UserAgentFormat    string
	DialTimeout        time.Duration
	MediaWait          time.Duration
	STUNTimeout        time.Duration
	PublicIPTimeout    time.Duration
	STUNServers        []string
}

type trunkRuntimeConfig struct {
	UserAgent          string
	RegisterEnabled    bool
	RegisterExpiry     time.Duration
	RegisterRetryDelay time.Duration
	UnregisterTimeout  time.Duration
	AllowRegHeaders    []string

	AuthUsername string

	OutboundProxy string

	BindHostOverride string
	BindPortOverride int
	PublicAddress    string
	STUNEnabled      bool
	STUNServers      []string

	Codecs   []media.Codec
	SRTPMode sip_trunk.SRTPMode

	DialPrefix      string
	DialStripDigits int
	NumberFormat    sip_trunk.NumberFormat
	HideCallerID    bool

	DialTimeout time.Duration
	MediaWait   time.Duration

	SessionTimerEnabled bool
	SessionTimerSeconds int
	MinSESeconds        int
	SessionRefresher    sip_trunk.SessionRefresher

	ExtraInviteHeaders []sip.Header
}

var defaultAllowMethods = []string{
	"INVITE", "ACK", "CANCEL", "BYE", "OPTIONS", "INFO", "UPDATE", "REFER", "NOTIFY",
}

func newTrunkRuntimeConfig(t *sip_trunk.SIPTrunk, d trunkDefaults, logger *log.Logger) trunkRuntimeConfig {
	cfg := trunkRuntimeConfig{
		UserAgent:          fmt.Sprintf(d.UserAgentFormat, t.ID),
		RegisterEnabled:    true,
		RegisterExpiry:     d.RegisterExpiry,
		RegisterRetryDelay: d.RegisterRetryDelay,
		UnregisterTimeout:  d.UnregisterTimeout,
		AllowRegHeaders:    defaultAllowMethods,
		AuthUsername:       t.Username,
		STUNEnabled:        true,
		STUNServers:        d.STUNServers,
		SRTPMode:           sip_trunk.SRTPModeDisabled,
		NumberFormat:       sip_trunk.NumberFormatPassthrough,
		DialTimeout:        d.DialTimeout,
		MediaWait:          d.MediaWait,
	}

	if v := strDeref(t.UserAgent); v != "" {
		cfg.UserAgent = v
	}
	if t.RegisterEnabled != nil {
		cfg.RegisterEnabled = *t.RegisterEnabled
	}
	if t.RegisterExpirySeconds != nil && *t.RegisterExpirySeconds > 0 {
		cfg.RegisterExpiry = time.Duration(*t.RegisterExpirySeconds) * time.Second
	}
	if t.RegisterRetrySeconds != nil && *t.RegisterRetrySeconds > 0 {
		cfg.RegisterRetryDelay = time.Duration(*t.RegisterRetrySeconds) * time.Second
	}
	if v := strDeref(t.AuthUsername); v != "" {
		cfg.AuthUsername = v
	}
	if v := strDeref(t.OutboundProxy); v != "" {
		cfg.OutboundProxy = v
	}
	if v := strDeref(t.BindHost); v != "" {
		cfg.BindHostOverride = v
	}
	if t.BindPort != nil && *t.BindPort > 0 {
		cfg.BindPortOverride = *t.BindPort
	}
	if v := strDeref(t.PublicAddress); v != "" {
		cfg.PublicAddress = v
	}
	if t.StunEnabled != nil {
		cfg.STUNEnabled = *t.StunEnabled
	}
	if len(t.StunServers) > 0 {
		cfg.STUNServers = append([]string(nil), t.StunServers...)
	}

	cfg.Codecs = resolveCodecList(t, logger)
	if t.SRTPMode != nil {
		cfg.SRTPMode = *t.SRTPMode
	}

	if v := strDeref(t.DialPrefix); v != "" {
		cfg.DialPrefix = v
	}
	if t.DialStripDigits != nil && *t.DialStripDigits > 0 {
		cfg.DialStripDigits = *t.DialStripDigits
	}
	if t.NumberFormat != nil {
		cfg.NumberFormat = *t.NumberFormat
	}
	if t.HideCallerID != nil {
		cfg.HideCallerID = *t.HideCallerID
	}
	if t.RingingTimeoutSeconds != nil && *t.RingingTimeoutSeconds > 0 {
		cfg.DialTimeout = time.Duration(*t.RingingTimeoutSeconds) * time.Second
	}

	if t.SessionTimerEnabled != nil {
		cfg.SessionTimerEnabled = *t.SessionTimerEnabled
	}
	if t.SessionTimerSeconds != nil {
		cfg.SessionTimerSeconds = *t.SessionTimerSeconds
	}
	if t.MinSESeconds != nil {
		cfg.MinSESeconds = *t.MinSESeconds
	}
	if t.SessionRefresher != nil {
		cfg.SessionRefresher = *t.SessionRefresher
	}

	cfg.ExtraInviteHeaders = buildInviteHeaders(cfg)
	return cfg
}

func resolveCodecList(t *sip_trunk.SIPTrunk, logger *log.Logger) []media.Codec {
	te := CodecTelephoneEvent
	if t.DTMFPayloadType != nil && *t.DTMFPayloadType > 0 && *t.DTMFPayloadType < 128 {
		te.PayloadType = uint8(*t.DTMFPayloadType)
	}

	ptime := time.Duration(0)
	if t.PTimeMs != nil && *t.PTimeMs > 0 {
		ptime = time.Duration(*t.PTimeMs) * time.Millisecond
	}

	applyPTime := func(c media.Codec) media.Codec {
		if ptime > 0 && c.Name != te.Name {
			c.SampleDur = ptime
		}
		return c
	}

	if len(t.Codecs) > 0 {
		out := make([]media.Codec, 0, len(t.Codecs)+1)
		seen := make(map[string]bool, len(t.Codecs)+1)
		for _, id := range t.Codecs {
			c, ok := codecByID(id, te)
			if !ok || seen[c.Name] {
				continue
			}
			seen[c.Name] = true
			out = append(out, applyPTime(c))
		}

		if !seen[te.Name] {
			out = append(out, te)
		}
		if logger != nil {
			logger.Printf("SIP codec list (per-trunk override): %d codecs", len(out))
		}
		return out
	}

	// Default offer profile. We only advertise codecs the media plane can
	// actually encode/decode (G.711 µ-law / A-law); G.722 and Opus were removed
	// end-to-end, so offering them is impossible here (see §0/§8.4/§9.1).
	//
	// PCMA is listed FIRST: Brazil's PSTN is A-law-native, so preferring it
	// avoids an extra µ-law↔A-law transcode at the carrier. This is now safe
	// because every channel encodes with the codec carried on the media session
	// (voip.MediaSession.NegotiatedCodec) — bridge, CRM, dialer and workflow all
	// emit A-law when PCMA is negotiated, so the wire label always matches the
	// bytes. Per-trunk `Codecs` config overrides this entirely.
	base := []media.Codec{
		applyPTime(CodecPCMA),
		applyPTime(CodecPCMU),
		te,
	}
	if logger != nil {
		logger.Printf("SIP codec list: using system default profile (%d codecs, PCMA-first)", len(base))
	}
	return base
}

func codecByID(id sip_trunk.CodecID, telephoneEvent media.Codec) (media.Codec, bool) {
	switch id {
	case sip_trunk.CodecIDPCMU:
		return CodecPCMU, true
	case sip_trunk.CodecIDPCMA:
		return CodecPCMA, true
	case sip_trunk.CodecIDTelephoneEvent:
		return telephoneEvent, true
	}
	// CodecIDOpus / CodecIDG722 are deprecated and deliberately unmapped: the
	// media plane is G.711-only, so any legacy per-trunk config naming them is
	// silently ignored rather than offered. See docs/SIP_AUDIO_PIPELINE.md §9.1.
	return media.Codec{}, false
}

func buildInviteHeaders(cfg trunkRuntimeConfig) []sip.Header {
	var headers []sip.Header

	if cfg.SessionTimerEnabled {
		exp := cfg.SessionTimerSeconds
		if exp <= 0 {
			exp = 1800
		}
		seVal := fmt.Sprintf("%d", exp)
		refresher := strings.ToLower(string(cfg.SessionRefresher))
		if refresher == "uac" || refresher == "uas" {
			seVal = fmt.Sprintf("%d;refresher=%s", exp, refresher)
		}
		headers = append(headers,
			sip.NewHeader("Session-Expires", seVal),
			sip.NewHeader("Supported", "timer"),
		)
		minSE := cfg.MinSESeconds
		if minSE <= 0 {
			minSE = 90
		}
		headers = append(headers, sip.NewHeader("Min-SE", fmt.Sprintf("%d", minSE)))
	}

	if cfg.HideCallerID {

		headers = append(headers, sip.NewHeader("Privacy", "id"))
	}

	return headers
}

func (cfg trunkRuntimeConfig) ApplyDialPlan(target string) string {
	if target == "" {
		return target
	}

	out := target

	if cfg.DialStripDigits > 0 && cfg.DialStripDigits < len(out) {
		out = out[cfg.DialStripDigits:]
	}

	switch cfg.NumberFormat {
	case sip_trunk.NumberFormatE164:
		if !strings.HasPrefix(out, "+") {
			out = "+" + out
		}
	case sip_trunk.NumberFormatNational, sip_trunk.NumberFormatLocal:
		out = strings.TrimPrefix(out, "+")
	}

	if cfg.DialPrefix != "" {
		out = cfg.DialPrefix + out
	}

	return out
}

func strDeref(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}

func stripHostPort(s string) string {
	if s == "" {
		return s
	}
	if host, _, err := net.SplitHostPort(s); err == nil {
		return host
	}
	return s
}
