package whatsappbusinessphone

type RequestVerificationRequest struct {
	Method   string `json:"method" example:"SMS"`
	Language string `json:"language" example:"pt_BR"`
}

type VerifyCodeRequest struct {
	Code string `json:"code" example:"123456"`
}

type UpdateBusinessProfileRequest struct {
	About                string   `json:"about"`
	Address              string   `json:"address"`
	Description          string   `json:"description"`
	Email                string   `json:"email"`
	ProfilePictureURL    string   `json:"profilePictureUrl"`
	ProfilePictureHandle string   `json:"profilePictureHandle"`
	Websites             []string `json:"websites"`
	Vertical             string   `json:"vertical"`
}

type RegisterPhoneRequest struct {
	Pin string `json:"pin" example:"123456"`
}

type ReleasePhoneRequest struct {
	ConfirmPhoneNumber string `json:"confirmPhoneNumber" example:"5511987654321"`
}

type AssignOwnerRequest struct {
	WorkspaceID string `json:"workspaceId" example:"ws_a1b2c3"`
}

type SetCallingStatusRequest struct {
	Enabled bool `json:"enabled" example:"true"`
}

type MessageResponse struct {
	Message string `json:"message" example:"WhatsApp Business phone number synchronized successfully"`
}

type CallingStatusResponse struct {
	Enabled bool `json:"enabled" example:"true"`
}

type VerticalOption struct {
	Value string `json:"value" example:"FINANCE"`
	Label string `json:"label" example:"Finance and Banking"`
}
