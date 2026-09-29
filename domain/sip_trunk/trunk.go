package sip_trunk

import (
	"net"
	"strings"
	"time"
)

type TrunkType string

const (
	TrunkTypeOutbound      TrunkType = "OUTBOUND"
	TrunkTypeInbound       TrunkType = "INBOUND"
	TrunkTypeBidirectional TrunkType = "BIDIRECTIONAL"
)

func (t TrunkType) Supports(direction CallDirection) bool {
	switch t {
	case TrunkTypeBidirectional:
		return direction == CallDirectionInbound || direction == CallDirectionOutbound
	case TrunkTypeOutbound:
		return direction == CallDirectionOutbound
	case TrunkTypeInbound:
		return direction == CallDirectionInbound
	}
	return false
}

type Transport string

const (
	TransportUDP Transport = "UDP"
	TransportTCP Transport = "TCP"
)

type RegistrationStatus string

const (
	RegistrationStatusUnregistered RegistrationStatus = "UNREGISTERED"
	RegistrationStatusRegistering  RegistrationStatus = "REGISTERING"
	RegistrationStatusRegistered   RegistrationStatus = "REGISTERED"
	RegistrationStatusFailed       RegistrationStatus = "FAILED"
)

type SIPTrunk struct {
	ID          string
	WorkspaceID string
	Name        string
	TrunkType   TrunkType

	Host      string
	Port      int
	Domain    string
	Transport Transport
	Username  string
	Password  string

	Enabled  bool
	Settings Settings

	RegistrationStatus RegistrationStatus
	LastError          string

	CreatedAt time.Time
	UpdatedAt time.Time
}

func (t *SIPTrunk) SignalingDomain() string {
	domain := t.Domain
	if domain == "" {
		domain = t.Host
	}
	if host, _, err := net.SplitHostPort(domain); err == nil {
		return host
	}
	return domain
}

func (t *SIPTrunk) AuthUsername() string {
	if user := strings.TrimSpace(t.Settings.AuthUsername); user != "" {
		return user
	}
	return t.Username
}

func (t *SIPTrunk) UpdateRegistrationStatus(status RegistrationStatus, lastError string) {
	t.RegistrationStatus = status
	t.LastError = lastError
	t.UpdatedAt = time.Now()
}

type SIPTrunkStatusUpdate struct {
	TrunkID   string
	Status    RegistrationStatus
	Error     string
	Timestamp time.Time
}
