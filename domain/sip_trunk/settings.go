package sip_trunk

import (
	"fmt"
	"strings"
)

type Codec string

const (
	CodecPCMU           Codec = "PCMU"
	CodecPCMA           Codec = "PCMA"
	CodecOpus           Codec = "opus"
	CodecTelephoneEvent Codec = "telephone-event"
)

var supportedCodecs = []Codec{CodecPCMU, CodecPCMA, CodecOpus, CodecTelephoneEvent}

func ParseCodec(raw string) (Codec, error) {
	for _, codec := range supportedCodecs {
		if strings.EqualFold(string(codec), strings.TrimSpace(raw)) {
			return codec, nil
		}
	}
	return "", fmt.Errorf("%w: %q", ErrUnsupportedCodec, raw)
}

type SRTPMode string

const (
	SRTPModeDisabled SRTPMode = ""
	SRTPModeOptional SRTPMode = "OPTIONAL"
	SRTPModeRequired SRTPMode = "REQUIRED"
)

func (m SRTPMode) Offers() bool {
	return m == SRTPModeOptional || m == SRTPModeRequired
}

type DialPlan struct {
	StripPrefix string
	AddPrefix   string
}

func (p DialPlan) Apply(number string) string {
	dialed := strings.TrimSpace(number)
	if p.StripPrefix != "" {
		dialed = strings.TrimPrefix(dialed, p.StripPrefix)
	}
	return p.AddPrefix + dialed
}

type Settings struct {
	AuthUsername          string
	OutboundProxy         string
	SkipRegistration      bool
	RegisterExpirySeconds int
	DialPlan              DialPlan
	Codecs                []Codec
	SRTPMode              SRTPMode
	STUNEnabled           bool
	STUNServers           []string
	BindHost              string
	BindPort              int
	PublicAddress         string
	UserAgent             string
	ExtraInviteHeaders    map[string]string
	AllowRegisterHeaders  []string
	InboundAllowedSources []string
}

func (s Settings) Validate() error {
	if err := s.DialPlan.validate(); err != nil {
		return err
	}
	for _, codec := range s.Codecs {
		if _, err := ParseCodec(string(codec)); err != nil {
			return err
		}
	}
	if len(s.Codecs) > 0 && !s.carriesG711() {
		return ErrNoBridgeableCodec
	}
	switch s.SRTPMode {
	case SRTPModeDisabled, SRTPModeOptional, SRTPModeRequired:
	default:
		return fmt.Errorf("%w: %q", ErrUnsupportedSRTPMode, s.SRTPMode)
	}
	_, err := ParseInboundSources(s.InboundAllowedSources)
	return err
}

func (p DialPlan) validate() error {
	for _, prefix := range []string{p.StripPrefix, p.AddPrefix} {
		if prefix == "" {
			continue
		}
		if _, err := NormalizeDialString(prefix); err != nil || prefix != strings.TrimSpace(prefix) {
			return ErrInvalidDialPlan
		}
	}
	return nil
}

func (s Settings) carriesG711() bool {
	for _, codec := range s.Codecs {
		parsed, _ := ParseCodec(string(codec))
		if parsed == CodecPCMU || parsed == CodecPCMA {
			return true
		}
	}
	return false
}
