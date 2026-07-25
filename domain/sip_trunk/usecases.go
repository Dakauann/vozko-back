package sip_trunk

import (
	"context"
	"time"

	"vozko/domain/voip"
)

type CreateTrunkInput struct {
	WorkspaceID       string        `json:"-"`
	Name              string        `json:"name"`
	Description       string        `json:"description,omitempty"`
	Host              string        `json:"host"`
	Port              int           `json:"port"`
	Username          string        `json:"username"`
	Password          string        `json:"password"`
	Domain            string        `json:"domain,omitempty"`
	Transport         TransportType `json:"transport"`
	TrunkType         TrunkType     `json:"trunkType"`
	IsRotational      bool          `json:"isRotational"`
	PhoneNumber       string        `json:"phoneNumber,omitempty"`
	IsGloballyVisible bool          `json:"-"`

	// Actor is set server-side (never from client JSON) and drives the
	// authorization + self-service cap checks in the use case.
	Actor Actor `json:"-"`

	Advanced AdvancedTrunkConfig `json:"advanced,omitempty"`
}

type UpdateTrunkInput struct {
	ID                string         `json:"id"`
	Name              *string        `json:"name,omitempty"`
	Description       *string        `json:"description,omitempty"`
	Host              *string        `json:"host,omitempty"`
	Port              *int           `json:"port,omitempty"`
	Username          *string        `json:"username,omitempty"`
	Password          *string        `json:"password,omitempty"`
	Domain            *string        `json:"domain,omitempty"`
	Transport         *TransportType `json:"transport,omitempty"`
	TrunkType         *TrunkType     `json:"trunkType,omitempty"`
	IsRotational      *bool          `json:"isRotational,omitempty"`
	PhoneNumber       *string        `json:"phoneNumber,omitempty"`
	IsGloballyVisible *bool          `json:"-"`

	// Actor is set server-side (never from client JSON) and drives the
	// authorization check in the use case.
	Actor Actor `json:"-"`

	Advanced *AdvancedTrunkConfig `json:"advanced,omitempty"`
}

type AdvancedTrunkConfig struct {
	OutboundProxy *string `json:"outboundProxy,omitempty"`
	AuthUsername  *string `json:"authUsername,omitempty"`
	UserAgent     *string `json:"userAgent,omitempty"`

	RegisterEnabled       *bool             `json:"registerEnabled,omitempty"`
	RegisterExpirySeconds *int              `json:"registerExpirySeconds,omitempty"`
	RegisterRetrySeconds  *int              `json:"registerRetrySeconds,omitempty"`
	SessionTimerEnabled   *bool             `json:"sessionTimerEnabled,omitempty"`
	SessionTimerSeconds   *int              `json:"sessionTimerSeconds,omitempty"`
	MinSESeconds          *int              `json:"minSESeconds,omitempty"`
	SessionRefresher      *SessionRefresher `json:"sessionRefresher,omitempty"`

	BindHost      *string  `json:"bindHost,omitempty"`
	BindPort      *int     `json:"bindPort,omitempty"`
	PublicAddress *string  `json:"publicAddress,omitempty"`
	StunServers   []string `json:"stunServers,omitempty"`
	StunEnabled   *bool    `json:"stunEnabled,omitempty"`

	Codecs          []CodecID `json:"codecs,omitempty"`
	PTimeMs         *int      `json:"ptimeMs,omitempty"`
	DTMFPayloadType *int      `json:"dtmfPayloadType,omitempty"`
	SRTPMode        *SRTPMode `json:"srtpMode,omitempty"`

	DialPrefix      *string       `json:"dialPrefix,omitempty"`
	DialStripDigits *int          `json:"dialStripDigits,omitempty"`
	NumberFormat    *NumberFormat `json:"numberFormat,omitempty"`
	HideCallerID    *bool         `json:"hideCallerId,omitempty"`

	RingingTimeoutSeconds *int `json:"ringingTimeoutSeconds,omitempty"`
}

func (c AdvancedTrunkConfig) ApplyTo(t *SIPTrunk) {
	if c.OutboundProxy != nil {
		t.OutboundProxy = c.OutboundProxy
	}
	if c.AuthUsername != nil {
		t.AuthUsername = c.AuthUsername
	}
	if c.UserAgent != nil {
		t.UserAgent = c.UserAgent
	}
	if c.RegisterEnabled != nil {
		t.RegisterEnabled = c.RegisterEnabled
	}
	if c.RegisterExpirySeconds != nil {
		t.RegisterExpirySeconds = c.RegisterExpirySeconds
	}
	if c.RegisterRetrySeconds != nil {
		t.RegisterRetrySeconds = c.RegisterRetrySeconds
	}
	if c.SessionTimerEnabled != nil {
		t.SessionTimerEnabled = c.SessionTimerEnabled
	}
	if c.SessionTimerSeconds != nil {
		t.SessionTimerSeconds = c.SessionTimerSeconds
	}
	if c.MinSESeconds != nil {
		t.MinSESeconds = c.MinSESeconds
	}
	if c.SessionRefresher != nil {
		t.SessionRefresher = c.SessionRefresher
	}
	if c.BindHost != nil {
		t.BindHost = c.BindHost
	}
	if c.BindPort != nil {
		t.BindPort = c.BindPort
	}
	if c.PublicAddress != nil {
		t.PublicAddress = c.PublicAddress
	}
	if c.StunServers != nil {
		t.StunServers = c.StunServers
	}
	if c.StunEnabled != nil {
		t.StunEnabled = c.StunEnabled
	}
	if c.Codecs != nil {
		t.Codecs = c.Codecs
	}
	if c.PTimeMs != nil {
		t.PTimeMs = c.PTimeMs
	}
	if c.DTMFPayloadType != nil {
		t.DTMFPayloadType = c.DTMFPayloadType
	}
	if c.SRTPMode != nil {
		t.SRTPMode = c.SRTPMode
	}
	if c.DialPrefix != nil {
		t.DialPrefix = c.DialPrefix
	}
	if c.DialStripDigits != nil {
		t.DialStripDigits = c.DialStripDigits
	}
	if c.NumberFormat != nil {
		t.NumberFormat = c.NumberFormat
	}
	if c.HideCallerID != nil {
		t.HideCallerID = c.HideCallerID
	}
	if c.RingingTimeoutSeconds != nil {
		t.RingingTimeoutSeconds = c.RingingTimeoutSeconds
	}
}

type CreateUseCase interface {
	Execute(input CreateTrunkInput) (*SIPTrunk, error)
}

type UpdateUseCase interface {
	Execute(input UpdateTrunkInput) (*SIPTrunk, error)
}

type DeleteUseCase interface {
	Execute(id string, actor Actor) error
}

type GetUseCase interface {
	Execute(id string) (*SIPTrunk, error)
}

type ListUseCase interface {
	Execute(page, pageSize int) ([]*SIPTrunk, int64, error)
}

type ListAccessibleUseCase interface {
	Execute(workspaceID string, grantedTrunkIDs []string, page, pageSize int) ([]*SIPTrunk, int64, error)
}

type ListByIDsUseCase interface {
	Execute(ids []string) ([]*SIPTrunk, error)
}

type EnableUseCase interface {
	Execute(id string, enabled bool, actor Actor) (*SIPTrunk, error)
}

type AssignOwnerUseCase interface {
	Execute(trunkID, workspaceID, assignedByUserID string) (*SIPTrunk, error)
}

type GetStatusUseCase interface {
	Execute(trunkID string) (*SIPTrunkStatusUpdate, error)
}

type TrunkManager interface {
	Start(ctx context.Context) error
	Stop() error
	RegisterTrunk(trunk *SIPTrunk) error
	RegisterTrunkAndWait(trunk *SIPTrunk, timeout time.Duration) error
	UnregisterTrunk(trunkID string) error
	RefreshTrunk(trunk *SIPTrunk) error
	GetActiveTrunks() []*SIPTrunk
	GetTrunkStatus(trunkID string) (*SIPTrunkStatusUpdate, error)
	SelectTrunkForCall(trunkType TrunkType) (*SIPTrunk, error)
	SetStatusCallback(fn func(update SIPTrunkStatusUpdate))

	WaitForRegistration(trunkID string, timeout time.Duration) error
	IsRegistered(trunkID string) bool

	Invite(ctx context.Context, trunkID string, input TrunkInviteInput) (TrunkCallSession, error)
	Hangup(ctx context.Context, trunkID string, callID string) error
	SetInboundInviteHandler(handler InboundInviteHandler)
}

type TrunkInviteInput struct {
	PhoneNumber string
	CallerID    string
	Recording   *voip.RecordingMeta
}

type TrunkCallSession struct {
	ID           string
	TrunkID      string
	PhoneNumber  string
	Direction    string
	StartedAt    time.Time
	LocalAddr    string
	RemoteAddr   string
	MediaSession voip.MediaSession
	Media        voip.MediaInfo
}

type InboundDialog interface {
	ID() string
	FromUser() string
	ToUser() string
	Trying() error
	Ringing() error
	Answer(ctx context.Context, recording *voip.RecordingMeta) (TrunkCallSession, error)
	Hangup(ctx context.Context) error
}

type InboundInvite struct {
	ID          string
	TrunkID     string
	WorkspaceID string
	FromNumber  string
	ToNumber    string
	ReceivedAt  time.Time
	Trunk       *SIPTrunk
	Dialog      InboundDialog
}

type InboundInviteHandler interface {
	HandleInboundInvite(ctx context.Context, invite InboundInvite) error
}
