package sip_trunk

import (
	"net"
	"strings"
)

func (t *SIPTrunk) Validate() error {
	if strings.TrimSpace(t.WorkspaceID) == "" {
		return ErrWorkspaceRequired
	}
	if strings.TrimSpace(t.Name) == "" {
		return ErrNameRequired
	}
	if err := validateHost(t.Host); err != nil {
		return err
	}
	if t.Port < 0 || t.Port > 65535 {
		return ErrInvalidPort
	}
	switch t.Transport {
	case TransportUDP, TransportTCP:
	default:
		return ErrUnsupportedTransport
	}
	switch t.TrunkType {
	case TrunkTypeOutbound, TrunkTypeInbound, TrunkTypeBidirectional:
	default:
		return ErrUnsupportedTrunkType
	}
	if !t.Settings.SkipRegistration && (strings.TrimSpace(t.Username) == "" || t.Password == "") {
		return ErrCredentialsRequired
	}
	return t.Settings.Validate()
}

func validateHost(host string) error {
	host = strings.TrimSpace(host)
	if host == "" {
		return ErrHostRequired
	}
	if strings.ContainsAny(host, ":/@ ") && net.ParseIP(host) == nil {
		return ErrInvalidHost
	}
	return nil
}
