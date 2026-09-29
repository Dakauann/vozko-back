package voipinfra

import (
	"fmt"
	"net"
	"sort"
	"strconv"
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
	UserAgent          string
	DialTimeout        time.Duration
	STUNServers        []string
}

type trunkRuntimeConfig struct {
	AuthUsername       string
	OutboundProxy      string
	RegisterEnabled    bool
	RegisterExpiry     time.Duration
	RegisterRetryDelay time.Duration
	UnregisterTimeout  time.Duration
	DialTimeout        time.Duration
	UserAgent          string
	Transport          string
	DialPlan           sip_trunk.DialPlan
	Codecs             []media.Codec
	SRTPMode           sip_trunk.SRTPMode
	STUNEnabled        bool
	STUNServers        []string
	BindHost           string
	BindPort           int
	PublicAddress      string
	ExtraInviteHeaders []headerPair
	AllowRegHeaders    []string
	InboundSources     sip_trunk.InboundSources
}

var diagoCodecs = map[sip_trunk.Codec]media.Codec{
	sip_trunk.CodecPCMU:           media.CodecAudioUlaw,
	sip_trunk.CodecPCMA:           media.CodecAudioAlaw,
	sip_trunk.CodecOpus:           media.CodecAudioOpus,
	sip_trunk.CodecTelephoneEvent: media.CodecTelephoneEvent8000,
}

func newTrunkRuntimeConfig(trunk *sip_trunk.SIPTrunk, defaults trunkDefaults) (trunkRuntimeConfig, error) {
	settings := trunk.Settings
	if err := settings.Validate(); err != nil {
		return trunkRuntimeConfig{}, err
	}
	transport, err := diagoTransport(trunk.Transport)
	if err != nil {
		return trunkRuntimeConfig{}, err
	}
	codecs, err := mapCodecs(settings.Codecs)
	if err != nil {
		return trunkRuntimeConfig{}, err
	}
	inboundSources, err := sip_trunk.ParseInboundSources(settings.InboundAllowedSources)
	if err != nil {
		return trunkRuntimeConfig{}, err
	}

	cfg := trunkRuntimeConfig{
		AuthUsername:       trunk.AuthUsername(),
		OutboundProxy:      strings.TrimSpace(settings.OutboundProxy),
		RegisterEnabled:    !settings.SkipRegistration,
		RegisterExpiry:     defaults.RegisterExpiry,
		RegisterRetryDelay: defaults.RegisterRetryDelay,
		UnregisterTimeout:  defaults.UnregisterTimeout,
		DialTimeout:        defaults.DialTimeout,
		UserAgent:          defaults.UserAgent,
		Transport:          transport,
		DialPlan:           settings.DialPlan,
		Codecs:             codecs,
		SRTPMode:           settings.SRTPMode,
		STUNEnabled:        settings.STUNEnabled,
		STUNServers:        defaults.STUNServers,
		BindHost:           strings.TrimSpace(settings.BindHost),
		BindPort:           settings.BindPort,
		PublicAddress:      strings.TrimSpace(settings.PublicAddress),
		ExtraInviteHeaders: sortedHeaderPairs(settings.ExtraInviteHeaders),
		AllowRegHeaders:    settings.AllowRegisterHeaders,
		InboundSources:     inboundSources,
	}
	if settings.RegisterExpirySeconds > 0 {
		cfg.RegisterExpiry = time.Duration(settings.RegisterExpirySeconds) * time.Second
	}
	if ua := strings.TrimSpace(settings.UserAgent); ua != "" {
		cfg.UserAgent = ua
	}
	if len(settings.STUNServers) > 0 {
		cfg.STUNServers = settings.STUNServers
	}
	return cfg, nil
}

func diagoTransport(transport sip_trunk.Transport) (string, error) {
	switch transport {
	case "", sip_trunk.TransportUDP:
		return "udp4", nil
	case sip_trunk.TransportTCP:
		return "tcp", nil
	}
	return "", fmt.Errorf("%w: %q", sip_trunk.ErrUnsupportedTransport, transport)
}

func mapCodecs(codecs []sip_trunk.Codec) ([]media.Codec, error) {
	if len(codecs) == 0 {
		return nil, nil
	}
	mapped := make([]media.Codec, 0, len(codecs))
	for _, raw := range codecs {
		codec, err := sip_trunk.ParseCodec(string(raw))
		if err != nil {
			return nil, err
		}
		mapped = append(mapped, diagoCodecs[codec])
	}
	return mapped, nil
}

type headerPair struct {
	Name  string
	Value string
}

func sortedHeaderPairs(headers map[string]string) []headerPair {
	pairs := make([]headerPair, 0, len(headers))
	for name, value := range headers {
		pairs = append(pairs, headerPair{Name: name, Value: value})
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].Name < pairs[j].Name })
	return pairs
}

func (c trunkRuntimeConfig) inviteHeaders(fromUser, domain string) []sip.Header {
	headers := make([]sip.Header, 0, len(c.ExtraInviteHeaders)+1)
	if fromUser != "" {
		headers = append(headers, &sip.FromHeader{Address: sip.Uri{User: fromUser, Host: domain}, Params: sip.NewParams()})
	}
	for _, pair := range c.ExtraInviteHeaders {
		headers = append(headers, sip.NewHeader(pair.Name, pair.Value))
	}
	return headers
}

const defaultSignalingPort = 5060

func registrarAddress(trunk *sip_trunk.SIPTrunk) string {
	port := trunk.Port
	if port == 0 {
		port = defaultSignalingPort
	}
	return net.JoinHostPort(trunk.Host, strconv.Itoa(port))
}

func stripHostPort(address string) string {
	if host, _, err := net.SplitHostPort(address); err == nil {
		return host
	}
	return address
}
