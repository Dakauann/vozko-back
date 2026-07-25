package mcp

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"vozko/brand"
	domainmcp "vozko/domain/agent/mcp"
	"vozko/infra/agent/mcp/client"
	"vozko/infra/agent/mcp/oauth"
	"vozko/infra/agent/mcp/vault"
)

type ProbeAuth struct {
	BearerToken string

	APIKey string

	HeaderName string
}

type Prober interface {
	Probe(ctx context.Context, url string, auth ProbeAuth) ([]domainmcp.Tool, error)
}

type ClientProber struct{}

func (ClientProber) Probe(ctx context.Context, url string, auth ProbeAuth) ([]domainmcp.Tool, error) {
	c := client.New(url)
	switch {
	case auth.BearerToken != "":
		c.BearerToken = auth.BearerToken
	case auth.APIKey != "":
		c.APIKey = auth.APIKey
		c.HeaderName = auth.HeaderName
	}
	if err := c.Initialize(ctx, brand.Active().Key, "1.0.0"); err != nil {
		return nil, err
	}
	return c.ListTools(ctx)
}

type OAuthDiscoverer interface {
	Discover(ctx context.Context, mcpURL string) (*oauth.DiscoveredConfig, error)
	Register(ctx context.Context, registrationURL string, req oauth.DCRRequest) (*oauth.DCRResponse, error)
}

type HTTPOAuthDiscoverer struct {
	HTTP interface {
		Do(*http.Request) (*http.Response, error)
	}
}

func (d HTTPOAuthDiscoverer) Discover(ctx context.Context, mcpURL string) (*oauth.DiscoveredConfig, error) {
	return oauth.Discover(ctx, httpAdapter{d.HTTP}, mcpURL)
}

func (d HTTPOAuthDiscoverer) Register(ctx context.Context, regURL string, req oauth.DCRRequest) (*oauth.DCRResponse, error) {
	return oauth.Register(ctx, httpAdapter{d.HTTP}, regURL, req)
}

type httpAdapter struct {
	inner interface {
		Do(*http.Request) (*http.Response, error)
	}
}

func (h httpAdapter) Do(r *http.Request) (*http.Response, error) {
	if h.inner == nil {
		return http.DefaultClient.Do(r)
	}
	return h.inner.Do(r)
}

type RegisterRemoteInput struct {
	ID          string
	WorkspaceID string
	Name        string
	URL         string
	AuthMode    domainmcp.AuthMode
	APIKey      string
}

type RegisterRemoteOutput struct {
	Server       *domainmcp.RemoteMCPServer
	AuthorizeURL string
}

type RegisterRemoteUseCase struct {
	Remotes     domainmcp.RemoteServerRepository
	Cache       domainmcp.ToolCacheRepository
	Vault       *vault.Vault
	Prober      Prober
	OAuth       OAuthDiscoverer
	StartOAuth  *StartOAuth2UseCase
	RedirectURL string
	ClientName  string
}

func (u *RegisterRemoteUseCase) Execute(ctx context.Context, in RegisterRemoteInput) (*RegisterRemoteOutput, error) {
	if !in.AuthMode.Valid() {
		return nil, domainmcp.ErrUnknownAuthMode
	}
	s, err := domainmcp.NewRemoteMCPServer(in.ID, in.WorkspaceID, in.Name, in.URL, domainmcp.TransportStreamableHTTP)
	if err != nil {
		return nil, err
	}
	s.Credential = &domainmcp.Credential{Mode: in.AuthMode}

	switch in.AuthMode {
	case domainmcp.AuthNone:
		return u.finishWithProbe(ctx, s, ProbeAuth{})
	case domainmcp.AuthAPIKey:
		if in.APIKey == "" {
			return nil, domainmcp.ErrCredentialRequired
		}
		sealed, sealErr := u.Vault.Seal([]byte(in.APIKey))
		if sealErr != nil {
			return nil, sealErr
		}
		s.Credential.Cipher = sealed
		s.Credential.KEKVersion = u.Vault.Version()
		return u.finishWithProbe(ctx, s, ProbeAuth{APIKey: in.APIKey})
	case domainmcp.AuthOAuth2:
		return u.startOAuth(ctx, s)
	}
	return nil, domainmcp.ErrUnknownAuthMode
}

func (u *RegisterRemoteUseCase) finishWithProbe(ctx context.Context, s *domainmcp.RemoteMCPServer, auth ProbeAuth) (*RegisterRemoteOutput, error) {
	tools, err := u.Prober.Probe(ctx, s.URL, auth)
	if err != nil {
		s.Status = domainmcp.StatusError
		_ = u.Remotes.Create(ctx, s)
		return nil, err
	}
	s.Status = domainmcp.StatusConnected
	now := domainmcp.Now()
	s.LastListedAt = &now
	if err := u.Remotes.Create(ctx, s); err != nil {
		return nil, err
	}
	cached := make([]domainmcp.CachedTool, 0, len(tools))
	sourceID := string(domainmcp.KindRemote) + ":" + s.ID
	for _, t := range tools {
		cached = append(cached, domainmcp.NewCachedTool(sourceID, s.WorkspaceID, t))
	}
	if err := u.Cache.Replace(ctx, sourceID, s.WorkspaceID, cached); err != nil {
		return nil, err
	}
	return &RegisterRemoteOutput{Server: s}, nil
}

func (u *RegisterRemoteUseCase) startOAuth(ctx context.Context, s *domainmcp.RemoteMCPServer) (*RegisterRemoteOutput, error) {
	if u.OAuth == nil || u.StartOAuth == nil {
		return nil, errors.New("mcp: oauth2 registration not configured")
	}
	if u.RedirectURL == "" {
		return nil, errors.New("mcp: oauth2 redirect_uri not configured")
	}
	disc, err := u.OAuth.Discover(ctx, s.URL)
	if err != nil {
		return nil, err
	}
	clientName := u.ClientName
	if clientName == "" {
		clientName = brand.Active().Name
	}

	var clientID, clientSecret string
	if disc.SupportsDCR {
		reg, err := u.OAuth.Register(ctx, disc.RegistrationURL, oauth.DCRRequest{
			ClientName:   clientName,
			RedirectURIs: []string{u.RedirectURL},
			Scope:        strings.Join(disc.Scopes, " "),
		})
		if err != nil {
			return nil, err
		}
		clientID = reg.ClientID
		clientSecret = reg.ClientSecret
	} else {
		return nil, errors.New("mcp: remote AS does not support dynamic client registration")
	}

	s.OAuth = &domainmcp.RemoteOAuthConfig{
		AuthzURL:        disc.AuthzURL,
		TokenURL:        disc.TokenURL,
		RegistrationURL: disc.RegistrationURL,
		Scopes:          disc.Scopes,
		Resource:        disc.Resource,
		ClientID:        clientID,
	}
	if clientSecret != "" {
		sealed, err := u.Vault.Seal([]byte(clientSecret))
		if err != nil {
			return nil, err
		}
		s.OAuth.ClientSecretCipher = sealed
		s.OAuth.ClientSecretKEK = uint32(u.Vault.Version())
	}
	s.Status = domainmcp.StatusPending
	if err := u.Remotes.Create(ctx, s); err != nil {
		return nil, err
	}

	out, err := u.StartOAuth.Execute(ctx, StartOAuth2Input{
		Kind:        "remote",
		WorkspaceID: s.WorkspaceID,
		BindingID:   s.ID,
	})
	if err != nil {
		return nil, err
	}
	return &RegisterRemoteOutput{Server: s, AuthorizeURL: out.AuthorizeURL}, nil
}
