package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	domainmcp "vozko/domain/agent/mcp"
	"vozko/infra/agent/mcp/oauth"
	"vozko/infra/agent/mcp/vault"
)

type OAuth2Config struct {
	AuthzURL     string
	TokenURL     string
	Scopes       []string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	UsePKCE      bool

	Resource string
}

type OAuth2ConfigResolver interface {
	Resolve(ctx context.Context, kind, workspaceID, bindingID string) (OAuth2Config, error)
}

type StartOAuth2Input struct {
	Kind        string
	WorkspaceID string
	BindingID   string
}

type StartOAuth2Output struct {
	AuthorizeURL string
	State        string
}

type StartOAuth2UseCase struct {
	Resolver OAuth2ConfigResolver
	Signer   *oauth.Signer
	Rand     io.Reader
}

func (u *StartOAuth2UseCase) Execute(ctx context.Context, in StartOAuth2Input) (StartOAuth2Output, error) {
	if in.WorkspaceID == "" {
		return StartOAuth2Output{}, domainmcp.ErrWorkspaceRequired
	}
	if in.BindingID == "" {
		return StartOAuth2Output{}, domainmcp.ErrServerKeyRequired
	}
	if in.Kind != "builtin" && in.Kind != "remote" {
		return StartOAuth2Output{}, errors.New("mcp: kind must be builtin or remote")
	}
	cfg, err := u.Resolver.Resolve(ctx, in.Kind, in.WorkspaceID, in.BindingID)
	if err != nil {
		return StartOAuth2Output{}, err
	}
	if cfg.AuthzURL == "" || cfg.TokenURL == "" || cfg.ClientID == "" || cfg.RedirectURL == "" {
		return StartOAuth2Output{}, errors.New("mcp: oauth2 config incomplete")
	}
	pkce, err := oauth.NewPKCE(u.Rand)
	if err != nil {
		return StartOAuth2Output{}, err
	}
	state := oauth.State{
		Kind:        in.Kind,
		WorkspaceID: in.WorkspaceID,
		BindingID:   in.BindingID,
		Verifier:    pkce.Verifier,
	}
	token, err := u.Signer.Sign(state)
	if err != nil {
		return StartOAuth2Output{}, err
	}
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", cfg.ClientID)
	q.Set("redirect_uri", cfg.RedirectURL)
	q.Set("state", token)
	if len(cfg.Scopes) > 0 {
		q.Set("scope", strings.Join(cfg.Scopes, " "))
	}
	if cfg.UsePKCE {
		q.Set("code_challenge", pkce.Challenge)
		q.Set("code_challenge_method", "S256")
	}
	if cfg.Resource != "" {
		q.Set("resource", cfg.Resource)
	}
	sep := "?"
	if strings.Contains(cfg.AuthzURL, "?") {
		sep = "&"
	}
	return StartOAuth2Output{
		AuthorizeURL: cfg.AuthzURL + sep + q.Encode(),
		State:        token,
	}, nil
}

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

type CompleteOAuth2Input struct {
	Code  string
	State string
}

type CompleteOAuth2UseCase struct {
	Resolver OAuth2ConfigResolver
	Signer   *oauth.Signer
	Bindings domainmcp.BuiltinBindingRepository
	Remotes  domainmcp.RemoteServerRepository
	Cache    domainmcp.ToolCacheRepository
	Prober   Prober
	Vault    *vault.Vault
	HTTP     interface {
		Do(*http.Request) (*http.Response, error)
	}
}

func (u *CompleteOAuth2UseCase) Execute(ctx context.Context, in CompleteOAuth2Input) error {
	if in.Code == "" || in.State == "" {
		return errors.New("mcp: code and state required")
	}
	st, err := u.Signer.Verify(in.State)
	if err != nil {
		return err
	}
	cfg, err := u.Resolver.Resolve(ctx, st.Kind, st.WorkspaceID, st.BindingID)
	if err != nil {
		return err
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", in.Code)
	form.Set("redirect_uri", cfg.RedirectURL)
	form.Set("client_id", cfg.ClientID)
	if cfg.ClientSecret != "" {
		form.Set("client_secret", cfg.ClientSecret)
	}
	if cfg.UsePKCE {
		form.Set("code_verifier", st.Verifier)
	}
	if cfg.Resource != "" {
		form.Set("resource", cfg.Resource)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	httpc := u.HTTP
	if httpc == nil {
		httpc = &http.Client{Timeout: 20 * time.Second}
	}
	resp, err := httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("mcp: token exchange failed: %s: %s", resp.Status, truncate(string(body), 200))
	}
	var tr TokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return fmt.Errorf("mcp: token decode: %w", err)
	}
	if tr.AccessToken == "" {
		return errors.New("mcp: empty access_token")
	}
	payload, err := domainmcp.EncodeOAuth2Secret(tr.AccessToken, tr.RefreshToken)
	if err != nil {
		return err
	}
	sealed, err := u.Vault.Seal(payload)
	if err != nil {
		return err
	}
	cred := &domainmcp.Credential{
		Mode:       domainmcp.AuthOAuth2,
		Cipher:     sealed,
		KEKVersion: u.Vault.Version(),
	}
	if tr.ExpiresIn > 0 {
		exp := domainmcp.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
		cred.ExpiresAt = &exp
		hint := exp.Add(-time.Duration(tr.ExpiresIn/5) * time.Second)
		cred.RefreshHint = &hint
	}
	switch st.Kind {
	case "builtin":
		b, err := u.Bindings.GetByID(ctx, st.WorkspaceID, st.BindingID)
		if err != nil {
			return err
		}
		b.Credential = cred
		b.Status = domainmcp.StatusConnected
		b.UpdatedAt = domainmcp.Now()
		return u.Bindings.Upsert(ctx, b)
	case "remote":
		s, err := u.Remotes.Get(ctx, st.WorkspaceID, st.BindingID)
		if err != nil {
			return err
		}
		s.Credential = cred
		s.Status = domainmcp.StatusConnected
		if u.Prober != nil && u.Cache != nil {
			tools, probeErr := u.Prober.Probe(ctx, s.URL, ProbeAuth{BearerToken: tr.AccessToken})
			if probeErr != nil {
				s.Status = domainmcp.StatusError
				_ = u.Remotes.Update(ctx, s)
				return fmt.Errorf("mcp: probe after oauth: %w", probeErr)
			}
			now := domainmcp.Now()
			s.LastListedAt = &now
			cached := make([]domainmcp.CachedTool, 0, len(tools))
			sourceID := string(domainmcp.KindRemote) + ":" + s.ID
			for _, t := range tools {
				cached = append(cached, domainmcp.NewCachedTool(sourceID, s.WorkspaceID, t))
			}
			if err := u.Cache.Replace(ctx, sourceID, s.WorkspaceID, cached); err != nil {
				return err
			}
		}
		return u.Remotes.Update(ctx, s)
	default:
		return errors.New("mcp: unknown state kind")
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

type EnvResolver struct {
	Catalog     BuiltinCatalog
	Bindings    domainmcp.BuiltinBindingRepository
	RedirectURL string
}

func (e *EnvResolver) Resolve(ctx context.Context, kind, workspaceID, bindingID string) (OAuth2Config, error) {
	if kind != "builtin" {
		return OAuth2Config{}, errors.New("mcp: EnvResolver only handles builtin")
	}
	if e.Bindings == nil {
		return OAuth2Config{}, errors.New("mcp: EnvResolver.Bindings not configured")
	}
	b, err := e.Bindings.GetByID(ctx, workspaceID, bindingID)
	if err != nil {
		return OAuth2Config{}, err
	}
	desc, ok := e.Catalog.Descriptor(b.ServerKey)
	if !ok {
		return OAuth2Config{}, fmt.Errorf("%w: %s", domainmcp.ErrServerKeyRequired, b.ServerKey)
	}
	s := desc.AuthSpec
	if s.Mode != domainmcp.AuthOAuth2 {
		return OAuth2Config{}, domainmcp.ErrUnknownAuthMode
	}
	return OAuth2Config{
		AuthzURL:     s.AuthzURL,
		TokenURL:     s.TokenURL,
		Scopes:       s.Scopes,
		ClientID:     os.Getenv(s.ClientIDEnv),
		ClientSecret: os.Getenv(s.ClientSecretEnv),
		RedirectURL:  e.RedirectURL,
		UsePKCE:      s.UsePKCE,
	}, nil
}

type RemoteOAuthResolver struct {
	Remotes     domainmcp.RemoteServerRepository
	Vault       *mcpVaultUnsealer
	RedirectURL string
}

type mcpVaultUnsealer = vault.Vault

func (r *RemoteOAuthResolver) Resolve(ctx context.Context, kind, workspaceID, bindingID string) (OAuth2Config, error) {
	if kind != "remote" {
		return OAuth2Config{}, errors.New("mcp: RemoteOAuthResolver only handles remote")
	}
	if r.Remotes == nil {
		return OAuth2Config{}, errors.New("mcp: RemoteOAuthResolver.Remotes not configured")
	}
	s, err := r.Remotes.Get(ctx, workspaceID, bindingID)
	if err != nil {
		return OAuth2Config{}, err
	}
	if s.OAuth == nil || s.OAuth.AuthzURL == "" || s.OAuth.TokenURL == "" || s.OAuth.ClientID == "" {
		return OAuth2Config{}, errors.New("mcp: remote server missing oauth config")
	}
	clientSecret := ""
	if len(s.OAuth.ClientSecretCipher) > 0 && r.Vault != nil {
		plain, err := r.Vault.Open(s.OAuth.ClientSecretCipher)
		if err != nil {
			return OAuth2Config{}, fmt.Errorf("mcp: unseal client_secret: %w", err)
		}
		clientSecret = string(plain)
	}
	return OAuth2Config{
		AuthzURL:     s.OAuth.AuthzURL,
		TokenURL:     s.OAuth.TokenURL,
		Scopes:       s.OAuth.Scopes,
		ClientID:     s.OAuth.ClientID,
		ClientSecret: clientSecret,
		RedirectURL:  r.RedirectURL,
		UsePKCE:      true,
		Resource:     s.OAuth.Resource,
	}, nil
}

type ChainResolver struct {
	Builtin OAuth2ConfigResolver
	Remote  OAuth2ConfigResolver
}

func (c *ChainResolver) Resolve(ctx context.Context, kind, workspaceID, bindingID string) (OAuth2Config, error) {
	switch kind {
	case "builtin":
		if c.Builtin == nil {
			return OAuth2Config{}, errors.New("mcp: builtin resolver not configured")
		}
		return c.Builtin.Resolve(ctx, kind, workspaceID, bindingID)
	case "remote":
		if c.Remote == nil {
			return OAuth2Config{}, errors.New("mcp: remote resolver not configured")
		}
		return c.Remote.Resolve(ctx, kind, workspaceID, bindingID)
	default:
		return OAuth2Config{}, fmt.Errorf("mcp: unknown kind %q", kind)
	}
}
