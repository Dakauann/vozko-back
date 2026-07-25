package oauth

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
)

type ProtectedResourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
	ScopesSupported      []string `json:"scopes_supported"`
}

type AuthzServerMetadata struct {
	Issuer                        string   `json:"issuer"`
	AuthorizationEndpoint         string   `json:"authorization_endpoint"`
	TokenEndpoint                 string   `json:"token_endpoint"`
	RegistrationEndpoint          string   `json:"registration_endpoint"`
	ScopesSupported               []string `json:"scopes_supported"`
	CodeChallengeMethodsSupported []string `json:"code_challenge_methods_supported"`
	GrantTypesSupported           []string `json:"grant_types_supported"`
	TokenEndpointAuthMethods      []string `json:"token_endpoint_auth_methods_supported"`
}

type DiscoveredConfig struct {
	Resource            string
	AuthorizationServer string
	AuthzURL            string
	TokenURL            string
	RegistrationURL     string
	Scopes              []string
	SupportsPKCE        bool
	SupportsDCR         bool
	TokenAuthMethods    []string
}

type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

func defaultHTTP(h httpDoer) httpDoer {
	if h != nil {
		return h
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func Discover(ctx context.Context, httpc httpDoer, mcpURL string) (*DiscoveredConfig, error) {
	httpc = defaultHTTP(httpc)
	origin, err := originOf(mcpURL)
	if err != nil {
		return nil, err
	}
	res := &DiscoveredConfig{Resource: mcpURL}

	prm, _ := fetchProtectedResource(ctx, httpc, origin)
	asURL := ""
	if prm != nil {
		if prm.Resource != "" {
			res.Resource = prm.Resource
		}
		if len(prm.AuthorizationServers) > 0 {
			asURL = prm.AuthorizationServers[0]
		}
		if len(prm.ScopesSupported) > 0 {
			res.Scopes = prm.ScopesSupported
		}
	}
	if asURL == "" {
		asURL = origin
	}
	res.AuthorizationServer = asURL

	as, err := fetchAuthzServer(ctx, httpc, asURL)
	if err != nil {
		return nil, fmt.Errorf("oauth discovery: %w", err)
	}
	if as.AuthorizationEndpoint == "" || as.TokenEndpoint == "" {
		return nil, errors.New("oauth discovery: AS metadata missing endpoints")
	}
	res.AuthzURL = as.AuthorizationEndpoint
	res.TokenURL = as.TokenEndpoint
	res.RegistrationURL = as.RegistrationEndpoint
	res.SupportsDCR = as.RegistrationEndpoint != ""
	res.TokenAuthMethods = as.TokenEndpointAuthMethods
	if len(res.Scopes) == 0 && len(as.ScopesSupported) > 0 {
		res.Scopes = as.ScopesSupported
	}
	for _, m := range as.CodeChallengeMethodsSupported {
		if strings.EqualFold(m, "S256") {
			res.SupportsPKCE = true
			break
		}
	}
	if len(as.CodeChallengeMethodsSupported) == 0 {

		res.SupportsPKCE = true
	}
	return res, nil
}

func originOf(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid URL: %q", rawURL)
	}
	return u.Scheme + "://" + u.Host, nil
}

func fetchProtectedResource(ctx context.Context, httpc httpDoer, origin string) (*ProtectedResourceMetadata, error) {
	var out ProtectedResourceMetadata
	if err := fetchJSON(ctx, httpc, origin+"/.well-known/oauth-protected-resource", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func fetchAuthzServer(ctx context.Context, httpc httpDoer, asURL string) (*AuthzServerMetadata, error) {
	candidates := []string{
		asURL + "/.well-known/oauth-authorization-server",
		asURL + "/.well-known/openid-configuration",
	}
	var firstErr error
	for _, u := range candidates {
		var out AuthzServerMetadata
		if err := fetchJSON(ctx, httpc, u, &out); err == nil {
			return &out, nil
		} else if firstErr == nil {
			firstErr = err
		}
	}
	return nil, firstErr
}

func fetchJSON(ctx context.Context, httpc httpDoer, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("GET %s: %s: %s", url, resp.Status, string(body))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
