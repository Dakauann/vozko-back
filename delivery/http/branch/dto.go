package branch

import (
	branchdomain "vozko/domain/branch"
)

type EnableBranchRequest struct {
	Enabled bool `json:"enabled" example:"true"`
}

type branchResponse struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspaceId"`
	MemberID    string `json:"memberId"`
	UserID      string `json:"userId"`

	SIPUser     string `json:"sipUser"`
	DisplayName string `json:"displayName,omitempty"`
	Realm       string `json:"realm"`

	Codecs      []branchdomain.CodecID `json:"codecs,omitempty"`
	MaxContacts int                    `json:"maxContacts"`
	DND         bool                   `json:"dnd"`

	Enabled            bool    `json:"enabled"`
	RegistrationStatus string  `json:"registrationStatus"`
	LastRegisteredAt   *string `json:"lastRegisteredAt,omitempty"`

	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type branchConnectionInfo struct {
	Server     string   `json:"server"`
	Port       int      `json:"port"`
	Transport  string   `json:"transport"`
	Realm      string   `json:"realm"`
	SIPUser    string   `json:"sipUser"`
	Codecs     []string `json:"codecs,omitempty"`
	Configured bool     `json:"configured"`
}

type branchSecretResponse struct {
	Branch     branchResponse       `json:"branch"`
	Secret     string               `json:"secret"`
	Realm      string               `json:"realm"`
	SIPUser    string               `json:"sipUser"`
	Connection branchConnectionInfo `json:"connection"`
}

func toBranchResponse(b *branchdomain.Branch) branchResponse {
	resp := branchResponse{
		ID:                 b.ID,
		WorkspaceID:        b.WorkspaceID,
		MemberID:           b.MemberID,
		UserID:             b.UserID,
		SIPUser:            b.SIPUser,
		DisplayName:        b.DisplayName,
		Realm:              b.Realm,
		Codecs:             b.Codecs,
		MaxContacts:        b.MaxContacts,
		DND:                b.DND,
		Enabled:            b.Enabled,
		RegistrationStatus: string(b.RegistrationStatus),
		CreatedAt:          b.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:          b.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}
	if b.LastRegisteredAt != nil {
		s := b.LastRegisteredAt.Format("2006-01-02T15:04:05Z")
		resp.LastRegisteredAt = &s
	}
	return resp
}
