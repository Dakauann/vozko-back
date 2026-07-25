package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	domainmcp "vozko/domain/agent/mcp"
	"vozko/infra/agent/mcp/vault"
)

var ErrNoRefreshToken = errors.New("mcp: no refresh_token stored; reconnect required")

type RefreshOAuth2UseCase struct {
	Resolver OAuth2ConfigResolver
	Remotes  domainmcp.RemoteServerRepository
	Bindings domainmcp.BuiltinBindingRepository
	Vault    *vault.Vault
	HTTP     interface {
		Do(*http.Request) (*http.Response, error)
	}
}

func (u *RefreshOAuth2UseCase) RefreshRemote(ctx context.Context, workspaceID, remoteID string) (string, error) {
	if u == nil || u.Remotes == nil || u.Vault == nil || u.Resolver == nil {
		return "", errors.New("mcp: RefreshOAuth2UseCase not configured")
	}
	s, err := u.Remotes.Get(ctx, workspaceID, remoteID)
	if err != nil {
		return "", err
	}
	if s.Credential == nil || s.Credential.Mode != domainmcp.AuthOAuth2 || len(s.Credential.Cipher) == 0 {
		return "", errors.New("mcp: not an oauth2 remote")
	}
	plain, err := u.Vault.Open(s.Credential.Cipher)
	if err != nil {
		return "", fmt.Errorf("mcp: unseal: %w", err)
	}
	sec := domainmcp.DecodeOAuth2Secret(plain)
	if sec.RefreshToken == "" {
		return "", ErrNoRefreshToken
	}
	cfg, err := u.Resolver.Resolve(ctx, "remote", workspaceID, remoteID)
	if err != nil {
		return "", err
	}
	newAccess, newRefresh, expiresIn, err := u.exchange(ctx, cfg, sec.RefreshToken)
	if err != nil {
		s.Status = domainmcp.StatusDisconnected
		_ = u.Remotes.Update(ctx, s)
		return "", err
	}

	if newRefresh == "" {
		newRefresh = sec.RefreshToken
	}
	payload, err := domainmcp.EncodeOAuth2Secret(newAccess, newRefresh)
	if err != nil {
		return "", err
	}
	sealed, err := u.Vault.Seal(payload)
	if err != nil {
		return "", err
	}
	s.Credential.Cipher = sealed
	s.Credential.KEKVersion = u.Vault.Version()
	if expiresIn > 0 {
		exp := domainmcp.Now().Add(time.Duration(expiresIn) * time.Second)
		s.Credential.ExpiresAt = &exp
		hint := exp.Add(-time.Duration(expiresIn/5) * time.Second)
		s.Credential.RefreshHint = &hint
	}
	s.Status = domainmcp.StatusConnected
	if err := u.Remotes.Update(ctx, s); err != nil {
		return "", err
	}
	return newAccess, nil
}

func (u *RefreshOAuth2UseCase) exchange(ctx context.Context, cfg OAuth2Config, refreshToken string) (string, string, int, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", cfg.ClientID)
	if cfg.ClientSecret != "" {
		form.Set("client_secret", cfg.ClientSecret)
	}
	if cfg.Resource != "" {
		form.Set("resource", cfg.Resource)
	}
	if len(cfg.Scopes) > 0 {
		form.Set("scope", strings.Join(cfg.Scopes, " "))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", "", 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	httpc := u.HTTP
	if httpc == nil {
		httpc = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := httpc.Do(req)
	if err != nil {
		return "", "", 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", 0, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", 0, fmt.Errorf("mcp: refresh failed: %s: %s", resp.Status, truncate(string(body), 200))
	}
	var tr TokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", "", 0, fmt.Errorf("mcp: refresh decode: %w", err)
	}
	if tr.AccessToken == "" {
		return "", "", 0, errors.New("mcp: refresh returned empty access_token")
	}
	return tr.AccessToken, tr.RefreshToken, tr.ExpiresIn, nil
}
