package googleoauth

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

	"vozko/domain/googleaccount"
	"vozko/domain/shared"
)

const requestTimeout = 15 * time.Second

type Endpoints struct {
	Authorize string
	Token     string
	UserInfo  string
	Revoke    string
}

var GoogleEndpoints = Endpoints{
	Authorize: "https://accounts.google.com/o/oauth2/v2/auth",
	Token:     "https://oauth2.googleapis.com/token",
	UserInfo:  "https://openidconnect.googleapis.com/v1/userinfo",
	Revoke:    "https://oauth2.googleapis.com/revoke",
}

type Config struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
	Endpoints    Endpoints
}

type Client struct {
	cfg  Config
	http *http.Client
}

var _ googleaccount.OAuthClient = (*Client)(nil)

func New(cfg Config) (*Client, error) {
	cfg.ClientID = strings.TrimSpace(cfg.ClientID)
	cfg.ClientSecret = strings.TrimSpace(cfg.ClientSecret)
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, errors.New("googleoauth: client id and secret are required")
	}
	if err := ValidateRedirectURI(cfg.RedirectURI); err != nil {
		return nil, err
	}
	if cfg.Endpoints == (Endpoints{}) {
		cfg.Endpoints = GoogleEndpoints
	}
	return &Client{cfg: cfg, http: &http.Client{Timeout: requestTimeout}}, nil
}

func ValidateRedirectURI(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("googleoauth: redirect uri %q is not an absolute url", raw)
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && shared.IsLoopbackHost(parsed.Hostname())) {
		return fmt.Errorf("googleoauth: redirect uri %q must use https, or http on localhost for development", raw)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("googleoauth: redirect uri %q must not carry a query or fragment", raw)
	}
	return nil
}

func (c *Client) AuthorizeURL(state string, scopes []string) string {
	params := url.Values{
		"client_id":              {c.cfg.ClientID},
		"redirect_uri":           {c.cfg.RedirectURI},
		"response_type":          {"code"},
		"scope":                  {strings.Join(scopes, " ")},
		"access_type":            {"offline"},
		"prompt":                 {"consent"},
		"include_granted_scopes": {"true"},
		"state":                  {state},
	}
	return c.cfg.Endpoints.Authorize + "?" + params.Encode()
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
}

func (c *Client) ExchangeCode(ctx context.Context, code string) (googleaccount.Grant, error) {
	return c.token(ctx, url.Values{
		"code":         {code},
		"redirect_uri": {c.cfg.RedirectURI},
		"grant_type":   {"authorization_code"},
	})
}

func (c *Client) Refresh(ctx context.Context, refreshToken string) (googleaccount.Grant, error) {
	return c.token(ctx, url.Values{
		"refresh_token": {refreshToken},
		"grant_type":    {"refresh_token"},
	})
}

func (c *Client) token(ctx context.Context, form url.Values) (googleaccount.Grant, error) {
	form.Set("client_id", c.cfg.ClientID)
	form.Set("client_secret", c.cfg.ClientSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.Endpoints.Token, strings.NewReader(form.Encode()))
	if err != nil {
		return googleaccount.Grant{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var body tokenResponse
	if err := c.do(req, &body); err != nil {
		return googleaccount.Grant{}, err
	}
	return googleaccount.Grant{
		AccessToken:  body.AccessToken,
		RefreshToken: body.RefreshToken,
		ExpiresIn:    time.Duration(body.ExpiresIn) * time.Second,
		Scopes:       strings.Fields(body.Scope),
	}, nil
}

func (c *Client) Profile(ctx context.Context, accessToken string) (googleaccount.Profile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.Endpoints.UserInfo, nil)
	if err != nil {
		return googleaccount.Profile{}, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	var body struct {
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := c.do(req, &body); err != nil {
		return googleaccount.Profile{}, err
	}
	email := googleaccount.NormalizeEmail(body.Email)
	if email == "" {
		return googleaccount.Profile{}, googleaccount.ErrEmailMissing
	}
	return googleaccount.Profile{Email: email, Name: strings.TrimSpace(body.Name)}, nil
}

func (c *Client) Revoke(ctx context.Context, token string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.Endpoints.Revoke, strings.NewReader(url.Values{"token": {token}}.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return c.do(req, nil)
}

func (c *Client) do(req *http.Request, out any) error {
	res, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("googleoauth: %s %s: %w", req.Method, req.URL.Path, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("googleoauth: read %s: %w", req.URL.Path, err)
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return newAPIError(res.StatusCode, raw)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("googleoauth: decode %s: %w", req.URL.Path, err)
	}
	return nil
}
