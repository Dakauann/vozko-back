package siptrunk

import (
	siptrunkdomain "vozko/domain/sip_trunk"
)

type AdminCreateInput struct {
	siptrunkdomain.CreateTrunkInput
	WorkspaceID       string `json:"workspaceId" example:"ws_a1b2c3"`
	IsGloballyVisible bool   `json:"isGloballyVisible" example:"false"`
}

type AdminUpdateInput struct {
	siptrunkdomain.UpdateTrunkInput
	IsGloballyVisible *bool `json:"isGloballyVisible,omitempty" example:"false"`
}

type AssignOwnerRequest struct {
	WorkspaceID string `json:"workspaceId" example:"ws_a1b2c3"`
}

type EnableTrunkRequest struct {
	Enabled bool `json:"enabled" example:"true"`
}

type sipTrunkResponse struct {
	ID                 string  `json:"id"`
	WorkspaceID        string  `json:"workspaceId,omitempty"`
	OwnerAssignedBy    string  `json:"ownerAssignedBy,omitempty"`
	OwnerAssignedAt    *string `json:"ownerAssignedAt,omitempty"`
	IsGloballyVisible  bool    `json:"isGloballyVisible"`
	Name               string  `json:"name"`
	Description        string  `json:"description,omitempty"`
	Host               string  `json:"host"`
	Port               int     `json:"port"`
	Username           string  `json:"username"`
	Domain             string  `json:"domain"`
	Transport          string  `json:"transport"`
	TrunkType          string  `json:"trunkType"`
	IsRotational       bool    `json:"isRotational"`
	PhoneNumber        string  `json:"phoneNumber,omitempty"`
	Enabled            bool    `json:"enabled"`
	RegistrationStatus string  `json:"registrationStatus"`
	LastError          string  `json:"lastError,omitempty"`
	LastRegisteredAt   *string `json:"lastRegisteredAt,omitempty"`
	CreatedAt          string  `json:"createdAt"`
	UpdatedAt          string  `json:"updatedAt"`

	Advanced *siptrunkdomain.AdvancedTrunkConfig `json:"advanced,omitempty"`
}

func buildAdvancedFromTrunk(t *siptrunkdomain.SIPTrunk) *siptrunkdomain.AdvancedTrunkConfig {
	c := siptrunkdomain.AdvancedTrunkConfig{
		OutboundProxy:         t.OutboundProxy,
		AuthUsername:          t.AuthUsername,
		UserAgent:             t.UserAgent,
		RegisterEnabled:       t.RegisterEnabled,
		RegisterExpirySeconds: t.RegisterExpirySeconds,
		RegisterRetrySeconds:  t.RegisterRetrySeconds,
		SessionTimerEnabled:   t.SessionTimerEnabled,
		SessionTimerSeconds:   t.SessionTimerSeconds,
		MinSESeconds:          t.MinSESeconds,
		SessionRefresher:      t.SessionRefresher,
		BindHost:              t.BindHost,
		BindPort:              t.BindPort,
		PublicAddress:         t.PublicAddress,
		StunServers:           t.StunServers,
		StunEnabled:           t.StunEnabled,
		Codecs:                t.Codecs,
		PTimeMs:               t.PTimeMs,
		DTMFPayloadType:       t.DTMFPayloadType,
		SRTPMode:              t.SRTPMode,
		DialPrefix:            t.DialPrefix,
		DialStripDigits:       t.DialStripDigits,
		NumberFormat:          t.NumberFormat,
		HideCallerID:          t.HideCallerID,
		RingingTimeoutSeconds: t.RingingTimeoutSeconds,
	}
	if advancedIsEmpty(c) {
		return nil
	}
	return &c
}

func advancedIsEmpty(c siptrunkdomain.AdvancedTrunkConfig) bool {
	return c.OutboundProxy == nil && c.AuthUsername == nil &&
		c.UserAgent == nil && c.RegisterEnabled == nil &&
		c.RegisterExpirySeconds == nil && c.RegisterRetrySeconds == nil &&
		c.SessionTimerEnabled == nil &&
		c.SessionTimerSeconds == nil && c.MinSESeconds == nil &&
		c.SessionRefresher == nil && c.BindHost == nil && c.BindPort == nil &&
		c.PublicAddress == nil &&
		len(c.StunServers) == 0 && c.StunEnabled == nil &&
		len(c.Codecs) == 0 &&
		c.PTimeMs == nil && c.DTMFPayloadType == nil && c.SRTPMode == nil &&
		c.DialPrefix == nil && c.DialStripDigits == nil &&
		c.NumberFormat == nil && c.HideCallerID == nil &&
		c.RingingTimeoutSeconds == nil
}

func toSIPTrunkResponse(trunk *siptrunkdomain.SIPTrunk) sipTrunkResponse {
	resp := sipTrunkResponse{
		ID:                 trunk.ID,
		WorkspaceID:        trunk.WorkspaceID,
		OwnerAssignedBy:    trunk.OwnerAssignedBy,
		IsGloballyVisible:  trunk.IsGloballyVisible,
		Name:               trunk.Name,
		Description:        trunk.Description,
		Host:               trunk.Host,
		Port:               trunk.Port,
		Username:           trunk.Username,
		Domain:             trunk.Domain,
		Transport:          string(trunk.Transport),
		TrunkType:          string(trunk.TrunkType),
		IsRotational:       trunk.IsRotational,
		PhoneNumber:        trunk.PhoneNumber,
		Enabled:            trunk.Enabled,
		RegistrationStatus: string(trunk.RegistrationStatus),
		LastError:          trunk.LastError,
		CreatedAt:          trunk.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:          trunk.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}
	if trunk.LastRegisteredAt != nil {
		s := trunk.LastRegisteredAt.Format("2006-01-02T15:04:05Z")
		resp.LastRegisteredAt = &s
	}
	if trunk.OwnerAssignedAt != nil {
		s := trunk.OwnerAssignedAt.Format("2006-01-02T15:04:05Z")
		resp.OwnerAssignedAt = &s
	}
	resp.Advanced = buildAdvancedFromTrunk(trunk)
	return resp
}

func toSIPTrunkResponseFor(trunk *siptrunkdomain.SIPTrunk, viewerWorkspaceID string, isPlatformAdmin bool) sipTrunkResponse {
	resp := toSIPTrunkResponse(trunk)
	if isPlatformAdmin || trunk.IsOwnedBy(viewerWorkspaceID) {
		return resp
	}

	resp.Host = ""
	resp.Port = 0
	resp.Username = ""
	resp.Domain = ""
	resp.PhoneNumber = ""

	resp.LastError = ""

	resp.WorkspaceID = ""
	resp.OwnerAssignedBy = ""
	resp.OwnerAssignedAt = nil

	resp.Advanced = nil
	return resp
}
