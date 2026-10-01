package sip_trunk

import (
	"net"
	"strings"
)

func (t *SIPTrunk) Validate() error {
	if err := t.ValidateWithoutPassword(); err != nil {
		return err
	}
	if t.registers() && t.Password == "" {
		return ErrCredentialsRequired
	}
	return nil
}

func (t *SIPTrunk) ValidateWithoutPassword() error {
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
	if t.registers() && strings.TrimSpace(t.Username) == "" {
		return ErrCredentialsRequired
	}
	return t.Settings.Validate()
}

func (t *SIPTrunk) registers() bool {
	return !t.Settings.SkipRegistration
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
