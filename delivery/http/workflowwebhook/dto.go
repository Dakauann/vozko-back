package workflowwebhook

const webhookMaxBodyBytes = 1 << 20

type webhookConfigRequest struct {
	AuthMode   string `json:"auth_mode" enums:"none,header_token,hmac" example:"hmac"`
	HeaderName string `json:"header_name" example:"X-Signature-256"`
	Method     string `json:"method" example:"POST"`
	Active     *bool  `json:"active" example:"true"`
}

type webhookConfigResponse struct {
	ID         string `json:"id" example:"a1b2c3d4"`
	WorkflowID string `json:"workflow_id" example:"wf_123"`
	URL        string `json:"url" example:"https://api.example.com/webhooks/workflow/tok_abc123"`
	AuthMode   string `json:"auth_mode" enums:"none,header_token,hmac" example:"hmac"`
	Secret     string `json:"secret,omitempty" example:"whsec_9f8e7d6c"`
	HeaderName string `json:"header_name,omitempty" example:"X-Signature-256"`
	Method     string `json:"method" example:"POST"`
	Active     bool   `json:"active" example:"true"`
}

type webhookTriggerRequest struct {
	EntryID   string `json:"entry_id,omitempty" example:"c7f1e2a0-9b3d-4a1e-8f2c-1d2e3f4a5b6c"`
	EntryType string `json:"entry_type,omitempty" enums:"whatsapp,unofficial_whatsapp,instagram,telegram,support" example:"whatsapp"`
	Phone     string `json:"phone,omitempty" example:"+5511998887777"`
}

type webhookTriggerResponse struct {
	Status string `json:"status" enums:"accepted,duplicate,already_running" example:"accepted"`
	RunID  string `json:"run_id,omitempty" example:"run_abc123"`
}
