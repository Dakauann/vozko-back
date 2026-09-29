package siptrunk

import (
	"time"

	"vozko/domain/sip_trunk"
)

type DialPlanDTO struct {
	StripPrefix string `json:"stripPrefix,omitempty" example:"0"`
	AddPrefix   string `json:"addPrefix,omitempty" example:"55"`
}

type TrunkSettingsDTO struct {
	AuthUsername          string            `json:"authUsername,omitempty" example:"1001"`
	OutboundProxy         string            `json:"outboundProxy,omitempty" example:"proxy.provedor.com.br:5060"`
	SkipRegistration      bool              `json:"skipRegistration" example:"false"`
	RegisterExpirySeconds int               `json:"registerExpirySeconds,omitempty" example:"3600"`
	DialPlan              DialPlanDTO       `json:"dialPlan"`
	Codecs                []string          `json:"codecs,omitempty" enums:"PCMU,PCMA,opus,telephone-event" example:"PCMA,PCMU"`
	SRTPMode              string            `json:"srtpMode,omitempty" enums:"OPTIONAL,REQUIRED" example:"OPTIONAL"`
	STUNEnabled           bool              `json:"stunEnabled" example:"false"`
	STUNServers           []string          `json:"stunServers,omitempty" example:"stun.l.google.com:19302"`
	BindHost              string            `json:"bindHost,omitempty" example:"0.0.0.0"`
	BindPort              int               `json:"bindPort,omitempty" example:"5063"`
	PublicAddress         string            `json:"publicAddress,omitempty" example:"203.0.113.10"`
	UserAgent             string            `json:"userAgent,omitempty" example:"Vozko"`
	ExtraInviteHeaders    map[string]string `json:"extraInviteHeaders,omitempty"`
	AllowRegisterHeaders  []string          `json:"allowRegisterHeaders,omitempty"`
	InboundAllowedSources []string          `json:"inboundAllowedSources,omitempty" example:"203.0.113.0/24"`
}

type CreateTrunkRequest struct {
	Name      string            `json:"name" example:"Operadora principal"`
	TrunkType string            `json:"trunkType" enums:"OUTBOUND,INBOUND,BIDIRECTIONAL" example:"BIDIRECTIONAL"`
	Host      string            `json:"host" example:"sip.provedor.com.br"`
	Port      int               `json:"port" example:"5060"`
	Domain    string            `json:"domain" example:"provedor.com.br"`
	Transport string            `json:"transport" enums:"UDP,TCP" example:"UDP"`
	Username  string            `json:"username" example:"1001"`
	Password  string            `json:"password" example:"s3nh4-do-tronco"`
	Enabled   *bool             `json:"enabled" example:"true"`
	Settings  *TrunkSettingsDTO `json:"settings"`
}

type UpdateTrunkRequest struct {
	Name      *string           `json:"name" example:"Operadora principal"`
	TrunkType *string           `json:"trunkType" enums:"OUTBOUND,INBOUND,BIDIRECTIONAL" example:"BIDIRECTIONAL"`
	Host      *string           `json:"host" example:"sip.provedor.com.br"`
	Port      *int              `json:"port" example:"5060"`
	Domain    *string           `json:"domain" example:"provedor.com.br"`
	Transport *string           `json:"transport" enums:"UDP,TCP" example:"UDP"`
	Username  *string           `json:"username" example:"1001"`
	Password  *string           `json:"password" example:"nova-s3nh4"`
	Enabled   *bool             `json:"enabled" example:"true"`
	Settings  *TrunkSettingsDTO `json:"settings"`
}

type TrunkResponse struct {
	ID                 string           `json:"id" example:"5f0c2b1e-8d2a-4c61-9b7e-1a2b3c4d5e6f"`
	Name               string           `json:"name" example:"Operadora principal"`
	TrunkType          string           `json:"trunkType" enums:"OUTBOUND,INBOUND,BIDIRECTIONAL" example:"BIDIRECTIONAL"`
	Host               string           `json:"host" example:"sip.provedor.com.br"`
	Port               int              `json:"port" example:"5060"`
	Domain             string           `json:"domain,omitempty" example:"provedor.com.br"`
	Transport          string           `json:"transport" enums:"UDP,TCP" example:"UDP"`
	Username           string           `json:"username" example:"1001"`
	HasPassword        bool             `json:"hasPassword" example:"true"`
	Enabled            bool             `json:"enabled" example:"true"`
	Settings           TrunkSettingsDTO `json:"settings"`
	RegistrationStatus string           `json:"registrationStatus" enums:"UNREGISTERED,REGISTERING,REGISTERED,FAILED" example:"REGISTERED"`
	LastError          string           `json:"lastError,omitempty" example:"401 Unauthorized"`
	CreatedAt          time.Time        `json:"createdAt"`
	UpdatedAt          time.Time        `json:"updatedAt"`
}

type ActiveCallResponse struct {
	ID          string     `json:"id" example:"sip-out-7d9f2c4a"`
	TrunkID     string     `json:"trunkId" example:"5f0c2b1e-8d2a-4c61-9b7e-1a2b3c4d5e6f"`
	Direction   string     `json:"direction" enums:"outbound,inbound" example:"outbound"`
	PhoneNumber string     `json:"phoneNumber" example:"5584999990000"`
	StartedAt   time.Time  `json:"startedAt"`
	AnsweredAt  *time.Time `json:"answeredAt,omitempty"`
}

type StatusResponse struct {
	Status string `json:"status" enums:"deleted,hung_up" example:"deleted"`
}

func (s *TrunkSettingsDTO) toDomain() sip_trunk.Settings {
	if s == nil {
		return sip_trunk.Settings{}
	}
	codecs := make([]sip_trunk.Codec, 0, len(s.Codecs))
	for _, codec := range s.Codecs {
		codecs = append(codecs, sip_trunk.Codec(codec))
	}
	return sip_trunk.Settings{
		AuthUsername:          s.AuthUsername,
		OutboundProxy:         s.OutboundProxy,
		SkipRegistration:      s.SkipRegistration,
		RegisterExpirySeconds: s.RegisterExpirySeconds,
		DialPlan:              sip_trunk.DialPlan{StripPrefix: s.DialPlan.StripPrefix, AddPrefix: s.DialPlan.AddPrefix},
		Codecs:                codecs,
		SRTPMode:              sip_trunk.SRTPMode(s.SRTPMode),
		STUNEnabled:           s.STUNEnabled,
		STUNServers:           s.STUNServers,
		BindHost:              s.BindHost,
		BindPort:              s.BindPort,
		PublicAddress:         s.PublicAddress,
		UserAgent:             s.UserAgent,
		ExtraInviteHeaders:    s.ExtraInviteHeaders,
		AllowRegisterHeaders:  s.AllowRegisterHeaders,
		InboundAllowedSources: s.InboundAllowedSources,
	}
}

func toSettingsDTO(s sip_trunk.Settings) TrunkSettingsDTO {
	codecs := make([]string, 0, len(s.Codecs))
	for _, codec := range s.Codecs {
		codecs = append(codecs, string(codec))
	}
	return TrunkSettingsDTO{
		AuthUsername:          s.AuthUsername,
		OutboundProxy:         s.OutboundProxy,
		SkipRegistration:      s.SkipRegistration,
		RegisterExpirySeconds: s.RegisterExpirySeconds,
		DialPlan:              DialPlanDTO{StripPrefix: s.DialPlan.StripPrefix, AddPrefix: s.DialPlan.AddPrefix},
		Codecs:                codecs,
		SRTPMode:              string(s.SRTPMode),
		STUNEnabled:           s.STUNEnabled,
		STUNServers:           s.STUNServers,
		BindHost:              s.BindHost,
		BindPort:              s.BindPort,
		PublicAddress:         s.PublicAddress,
		UserAgent:             s.UserAgent,
		ExtraInviteHeaders:    s.ExtraInviteHeaders,
		AllowRegisterHeaders:  s.AllowRegisterHeaders,
		InboundAllowedSources: s.InboundAllowedSources,
	}
}

func toTrunkDTO(t *sip_trunk.SIPTrunk) TrunkResponse {
	return TrunkResponse{
		ID:                 t.ID,
		Name:               t.Name,
		TrunkType:          string(t.TrunkType),
		Host:               t.Host,
		Port:               t.Port,
		Domain:             t.Domain,
		Transport:          string(t.Transport),
		Username:           t.Username,
		HasPassword:        t.Password != "",
		Enabled:            t.Enabled,
		Settings:           toSettingsDTO(t.Settings),
		RegistrationStatus: string(t.RegistrationStatus),
		LastError:          t.LastError,
		CreatedAt:          t.CreatedAt,
		UpdatedAt:          t.UpdatedAt,
	}
}

func toCallDTO(call sip_trunk.ActiveCall) ActiveCallResponse {
	dto := ActiveCallResponse{
		ID:          call.ID,
		TrunkID:     call.TrunkID,
		Direction:   string(call.Direction),
		PhoneNumber: call.PhoneNumber,
		StartedAt:   call.StartedAt,
	}
	if !call.AnsweredAt.IsZero() {
		answeredAt := call.AnsweredAt
		dto.AnsweredAt = &answeredAt
	}
	return dto
}

func optionalTrunkType(raw *string) *sip_trunk.TrunkType {
	if raw == nil {
		return nil
	}
	value := sip_trunk.TrunkType(*raw)
	return &value
}

func optionalTransport(raw *string) *sip_trunk.Transport {
	if raw == nil {
		return nil
	}
	value := sip_trunk.Transport(*raw)
	return &value
}

func optionalSettings(raw *TrunkSettingsDTO) *sip_trunk.Settings {
	if raw == nil {
		return nil
	}
	value := raw.toDomain()
	return &value
}
