package oauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

type DCRRequest struct {
	ClientName              string   `json:"client_name,omitempty"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types,omitempty"`
	ResponseTypes           []string `json:"response_types,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
	Scope                   string   `json:"scope,omitempty"`
	ApplicationType         string   `json:"application_type,omitempty"`
}

type DCRResponse struct {
	ClientID                string   `json:"client_id"`
	ClientSecret            string   `json:"client_secret,omitempty"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at,omitempty"`
	ClientSecretExpiresAt   int64    `json:"client_secret_expires_at,omitempty"`
	RedirectURIs            []string `json:"redirect_uris,omitempty"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
}

func Register(ctx context.Context, httpc httpDoer, registrationURL string, req DCRRequest) (*DCRResponse, error) {
	if registrationURL == "" {
		return nil, errors.New("oauth: registration_endpoint not advertised by AS")
	}
	if len(req.RedirectURIs) == 0 {
		return nil, errors.New("oauth: redirect_uris required for DCR")
	}
	if len(req.GrantTypes) == 0 {
		req.GrantTypes = []string{"authorization_code", "refresh_token"}
	}
	if len(req.ResponseTypes) == 0 {
		req.ResponseTypes = []string{"code"}
	}
	if req.TokenEndpointAuthMethod == "" {

		req.TokenEndpointAuthMethod = "none"
	}
	if req.ApplicationType == "" {
		req.ApplicationType = "web"
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, registrationURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json")
	httpc = defaultHTTP(httpc)
	resp, err := httpc.Do(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("oauth: DCR %s: %s", resp.Status, string(raw))
	}
	var out DCRResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("oauth: DCR decode: %w", err)
	}
	if out.ClientID == "" {
		return nil, errors.New("oauth: DCR response missing client_id")
	}
	return &out, nil
}
