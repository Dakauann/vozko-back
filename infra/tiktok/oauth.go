package tiktok

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	ttdomain "vozko/domain/tiktok"
)

const (
	authorizeHost = "www.tiktok.com"
	authorizePath = "/v2/auth/authorize"
)

var (
	ErrAuthorizeURLInvalid = errors.New("tiktok: TIKTOK_AUTHORIZE_URL must be the https://www.tiktok.com/v2/auth/authorize URL from the app page, without a state")
	ErrRedirectURIInvalid  = errors.New("tiktok: TIKTOK_REDIRECT_URI must be an absolute https URL ending with /, without query, fragment or port, 10 to 512 characters")
	ErrStateRequired       = errors.New("tiktok: an authorization needs a state")
)

func ParseAuthorizeURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, ErrAuthorizeURLInvalid
	}
	if u.Scheme != "https" || u.Host != authorizeHost || strings.TrimRight(u.Path, "/") != authorizePath || u.Fragment != "" {
		return nil, ErrAuthorizeURLInvalid
	}
	q := u.Query()
	if q.Has("state") || q.Get("disable_auto_auth") != "" && q.Get("disable_auto_auth") != "1" {
		return nil, ErrAuthorizeURLInvalid
	}
	return u, nil
}

func ValidateRedirectURI(raw string) error {
	raw = strings.TrimSpace(raw)
	if len(raw) < 10 || len(raw) > 512 || !strings.HasSuffix(raw, "/") {
		return ErrRedirectURIInvalid
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" || strings.Contains(raw, "#") {
		return ErrRedirectURIInvalid
	}
	return nil
}

func (c *Client) AuthorizeURL(state string) (string, error) {
	if strings.TrimSpace(state) == "" {
		return "", ErrStateRequired
	}
	u := *c.authorizeURL
	q := u.Query()
	q.Set("state", state)
	q.Set("disable_auto_auth", "1")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

type tokenResponse struct {
	AccessToken           string `json:"access_token"`
	ExpiresIn             int64  `json:"expires_in"`
	OpenID                string `json:"open_id"`
	RefreshToken          string `json:"refresh_token"`
	RefreshTokenExpiresIn int64  `json:"refresh_token_expires_in"`
	Scope                 string `json:"scope"`
}

func (t tokenResponse) grant() ttdomain.Grant {
	return ttdomain.Grant{
		OpenID:           strings.TrimSpace(t.OpenID),
		AccessToken:      strings.TrimSpace(t.AccessToken),
		RefreshToken:     strings.TrimSpace(t.RefreshToken),
		ExpiresIn:        time.Duration(t.ExpiresIn) * time.Second,
		RefreshExpiresIn: time.Duration(t.RefreshTokenExpiresIn) * time.Second,
		Scopes:           ttdomain.ParseScopes(t.Scope),
	}
}

func (c *Client) ExchangeCode(ctx context.Context, code string) (ttdomain.Grant, error) {
	if strings.TrimSpace(code) == "" {
		return ttdomain.Grant{}, fmt.Errorf("%w: authorization code is empty", ttdomain.ErrGrantIncomplete)
	}
	var out tokenResponse
	err := c.postJSON(ctx, request{path: "/tt_user/oauth2/token/", family: FamilyOAuth}, map[string]string{
		"client_id":     c.appID,
		"client_secret": c.appSecret,
		"grant_type":    "authorization_code",
		"auth_code":     strings.TrimSpace(code),
		"redirect_uri":  c.redirectURI,
	}, &out)
	if err != nil {
		return ttdomain.Grant{}, err
	}
	g := out.grant()
	return g, g.Validate()
}

func (c *Client) Refresh(ctx context.Context, refreshToken string) (ttdomain.Grant, error) {
	if strings.TrimSpace(refreshToken) == "" {
		return ttdomain.Grant{}, fmt.Errorf("%w: refresh token is empty", ttdomain.ErrGrantIncomplete)
	}
	var out tokenResponse
	err := c.postJSON(ctx, request{path: "/tt_user/oauth2/refresh_token/", family: FamilyOAuth}, map[string]string{
		"client_id":     c.appID,
		"client_secret": c.appSecret,
		"grant_type":    "refresh_token",
		"refresh_token": strings.TrimSpace(refreshToken),
	}, &out)
	if err != nil {
		return ttdomain.Grant{}, err
	}
	g := out.grant()
	return g, g.Validate()
}

func (c *Client) TokenInfo(ctx context.Context, accessToken string) (ttdomain.TokenInfo, error) {
	var out struct {
		CreatorID string `json:"creator_id"`
		Scope     string `json:"scope"`
	}
	err := c.postJSON(ctx, request{path: "/tt_user/token_info/get/", family: FamilyOAuth, retrySafe: true}, map[string]string{
		"app_id":       c.appID,
		"access_token": accessToken,
	}, &out)
	if err != nil {
		return ttdomain.TokenInfo{}, err
	}
	return ttdomain.TokenInfo{OpenID: strings.TrimSpace(out.CreatorID), Scopes: ttdomain.ParseScopes(out.Scope)}, nil
}

func (c *Client) Revoke(ctx context.Context, accessToken string) error {
	if strings.TrimSpace(accessToken) == "" {
		return nil
	}
	return c.postJSON(ctx, request{path: "/tt_user/oauth2/revoke/", family: FamilyOAuth, retrySafe: true}, map[string]string{
		"client_id":     c.appID,
		"client_secret": c.appSecret,
		"access_token":  accessToken,
	}, nil)
}
