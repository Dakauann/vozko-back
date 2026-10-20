package workflowsecrethttp

import "time"

type PutSecretRequest struct {
	Value string `json:"value" example:"s3nh4-do-portal"`
}

type SecretResponse struct {
	Name      string    `json:"name" example:"unifecaf_senha"`
	UpdatedAt time.Time `json:"updated_at"`
	UpdatedBy string    `json:"updated_by" example:"5b0c1d4e-9a7f-4f4a-8a52-3b7e1e2f9c10"`
}

type SecretListResponse struct {
	Secrets []SecretResponse `json:"secrets"`
}
