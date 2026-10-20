package mercadolivre

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	mldomain "vozko/domain/mercadolivre"
)

const DefaultAuthorizeURL = "https://auth.mercadolivre.com.br/authorization"

const exchangeLimiterKey = "ml:exchange"

var pkceVerifierPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)

var pkceChallengePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

var errPKCEVerifier = errors.New("mercadolivre: pkce verifier must be 43 to 128 unreserved characters")

func NewPKCEVerifier() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func PKCEChallenge(verifier string) (string, error) {
	if !pkceVerifierPattern.MatchString(verifier) {
		return "", errPKCEVerifier
	}
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

type OAuthConfig struct {
	AppID        string
	ClientSecret string
	RedirectURI  string
	AuthorizeURL string
}

type OAuthService struct {
	c   *Client
	cfg OAuthConfig
}

var _ mldomain.OAuthService = (*OAuthService)(nil)

func NewOAuthService(c *Client, cfg OAuthConfig) (*OAuthService, error) {
	if c == nil {
		return nil, errors.New("mercadolivre: oauth service requires a client")
	}
	cfg.AppID = strings.TrimSpace(cfg.AppID)
	cfg.RedirectURI = strings.TrimSpace(cfg.RedirectURI)
	if !mldomain.ValidNumericID(cfg.AppID) {
		return nil, errors.New("mercadolivre: oauth app id must be numeric")
	}
	if strings.TrimSpace(cfg.ClientSecret) == "" {
		return nil, errors.New("mercadolivre: oauth client secret is required")
	}
	u, err := url.Parse(cfg.RedirectURI)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("mercadolivre: redirect uri must be an https url without query: %q", cfg.RedirectURI)
	}
	authorize, err := endpointURL(cfg.AuthorizeURL, DefaultAuthorizeURL)
	if err != nil {
		return nil, fmt.Errorf("mercadolivre: authorize url: %w", err)
	}
	cfg.AuthorizeURL = authorize
	return &OAuthService{c: c, cfg: cfg}, nil
}

func (s *OAuthService) AuthorizeURL(state, codeChallenge string) (string, error) {
	if strings.TrimSpace(state) == "" {
		return "", errors.New("mercadolivre: authorize needs a state")
	}
	if !pkceChallengePattern.MatchString(codeChallenge) {
		return "", errors.New("mercadolivre: authorize needs an S256 code challenge")
	}
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", s.cfg.AppID)
	q.Set("redirect_uri", s.cfg.RedirectURI)
	q.Set("state", state)
	q.Set("code_challenge", codeChallenge)
	q.Set("code_challenge_method", "S256")
	return s.cfg.AuthorizeURL + "?" + q.Encode(), nil
}

type tokenResponse struct {
	AccessToken  string              `json:"access_token"`
	TokenType    string              `json:"token_type"`
	ExpiresIn    int64               `json:"expires_in"`
	Scope        string              `json:"scope"`
	UserID       mldomain.FlexibleID `json:"user_id"`
	RefreshToken string              `json:"refresh_token"`
}

func (s *OAuthService) ExchangeCode(ctx context.Context, code, codeVerifier string) (*mldomain.TokenGrant, error) {
	if strings.TrimSpace(code) == "" {
		return nil, errors.New("mercadolivre: authorization code is required")
	}
	if !pkceVerifierPattern.MatchString(codeVerifier) {
		return nil, errPKCEVerifier
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", s.cfg.AppID)
	form.Set("client_secret", s.cfg.ClientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", s.cfg.RedirectURI)
	form.Set("code_verifier", codeVerifier)
	return s.token(ctx, exchangeLimiterKey, form)
}

func (s *OAuthService) Refresh(ctx context.Context, creds mldomain.RefreshCredentials) (*mldomain.TokenGrant, error) {
	if !mldomain.ValidNumericID(creds.UserID) || strings.TrimSpace(creds.RefreshToken) == "" {
		return nil, mldomain.ErrCredentialsInvalid
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("client_id", s.cfg.AppID)
	form.Set("client_secret", s.cfg.ClientSecret)
	form.Set("refresh_token", creds.RefreshToken)
	return s.token(ctx, accountKey(creds.UserID), form)
}

func (s *OAuthService) token(ctx context.Context, limiterKey string, form url.Values) (*mldomain.TokenGrant, error) {
	var out tokenResponse
	_, err := s.c.sendJSON(ctx, request{
		method:      http.MethodPost,
		path:        "/oauth/token",
		family:      FamilyOAuth,
		anonymous:   true,
		limiterKey:  limiterKey,
		body:        []byte(form.Encode()),
		contentType: contentTypeForm,
	}, nil, &out)
	if err != nil {
		return nil, err
	}
	grant := &mldomain.TokenGrant{
		AccessToken:  strings.TrimSpace(out.AccessToken),
		RefreshToken: strings.TrimSpace(out.RefreshToken),
		ExpiresIn:    time.Duration(out.ExpiresIn) * time.Second,
		UserID:       out.UserID.String(),
		Scopes:       mldomain.ParseScopes(out.Scope),
	}
	if grant.AccessToken == "" || grant.RefreshToken == "" || !mldomain.ValidNumericID(grant.UserID) || grant.ExpiresIn <= 0 {
		return nil, &Error{Code: CodeDecode, Message: "token answer is missing the token pair, the user or the expiry"}
	}
	return grant, nil
}

func (s *OAuthService) Revoke(ctx context.Context, creds mldomain.Credentials) error {
	_, err := s.c.do(ctx, request{
		method: http.MethodDelete,
		path:   "/users/" + pathEscape(creds.UserID) + "/applications/" + pathEscape(s.cfg.AppID),
		family: FamilyOAuth,
		creds:  creds,
	})
	return err
}
