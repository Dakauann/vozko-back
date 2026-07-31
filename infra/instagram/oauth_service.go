package instagram

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	igdomain "vozko/domain/instagram"
)

// Business Login for Instagram spans three fixed hosts.
//
// These are constants rather than configuration on purpose: they are determined
// by the login path (Instagram Login, not Facebook Login), not by the deployment.
// Making them env-tunable would add a silent-breakage footgun — point one at the
// wrong host and the whole flow fails in a way that looks like a Meta outage —
// while the only real need for substitution is tests, which inject an HTTPClient
// instead.
const (
	AuthorizeHost = "www.instagram.com"
	TokenHost     = "api.instagram.com"
	GraphHost     = "graph.instagram.com"
)

// OAuthConfig configures Business Login for Instagram.
type OAuthConfig struct {
	AppID     string
	AppSecret string
	// RedirectURI must match a URI registered in the App Dashboard EXACTLY.
	//
	// It is deliberately NOT caller-supplied. In OAuth the redirect URI is the
	// security boundary: if a request could name its own, anyone able to reach the
	// start endpoint could have the authorization code — and therefore the
	// account's token — delivered to a host they control. Callers instead choose
	// where they land AFTER the callback, via the `returnPath` carried inside the
	// signed state and validated as a relative path.
	RedirectURI string
	// GraphVersion is the one genuinely deployment-owned value here: Meta sunsets
	// versions on published dates, so it must be bumpable (and roll-back-able)
	// without a code change.
	GraphVersion string

	HTTPClient *http.Client
}

type oauthService struct {
	cfg  OAuthConfig
	http *http.Client
}

// NewOAuthService builds the Instagram OAuth service.
func NewOAuthService(cfg OAuthConfig) igdomain.OAuthService {
	if cfg.GraphVersion == "" {
		cfg.GraphVersion = DefaultGraphVersion
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &oauthService{cfg: cfg, http: client}
}

// BuildAuthorizeURL builds the URL the user is redirected to.
//
// Note that scope is COMMA-separated here; the Facebook dialog accepts either
// form but the Instagram authorize endpoint does not. enable_fb_login defaults
// to true upstream, so it is set explicitly to keep onboarding on the pure
// Instagram-credentials path.
func (s *oauthService) BuildAuthorizeURL(state string) string {
	q := url.Values{}
	q.Set("client_id", s.cfg.AppID)
	q.Set("redirect_uri", s.cfg.RedirectURI)
	q.Set("response_type", "code")
	q.Set("scope", strings.Join(igdomain.RequiredScopes(), ","))
	q.Set("state", state)
	q.Set("enable_fb_login", "false")
	q.Set("force_reauth", "true")

	authorizeURL := "https://" + AuthorizeHost + "/oauth/authorize?" + q.Encode()

	// The redirect_uri and scope list are logged because "Invalid redirect_uri" is
	// the most common onboarding failure and is always a byte-level mismatch against
	// the App Dashboard allowlist. Neither value is a secret.
	log.Printf("[instagram-oauth] authorize url built (client_id=%s redirect_uri=%q scopes=%s)",
		s.cfg.AppID, s.cfg.RedirectURI, q.Get("scope"))

	return authorizeURL
}

// shortLivedResponse is the code-exchange response.
//
// It is ARRAY-WRAPPED ({"data":[{...}]}) unlike every other token response in
// the flow. Decoding it as a flat object yields a zero-valued token with no
// error, which is why the shape is asserted explicitly below.
type shortLivedResponse struct {
	Data []struct {
		AccessToken string         `json:"access_token"`
		UserID      graphID        `json:"user_id"`
		Permissions permissionList `json:"permissions"`
	} `json:"data"`

	// Flat fallback: accepted because the docs are inconsistent across pages,
	// and a silent zero token is far worse than tolerating both shapes.
	AccessToken string         `json:"access_token"`
	UserID      graphID        `json:"user_id"`
	Permissions permissionList `json:"permissions"`
}

func (s *oauthService) ExchangeCode(ctx context.Context, code string) (*igdomain.TokenGrant, error) {
	form := url.Values{}
	form.Set("client_id", s.cfg.AppID)
	form.Set("client_secret", s.cfg.AppSecret)
	form.Set("grant_type", "authorization_code")
	form.Set("redirect_uri", s.cfg.RedirectURI)
	form.Set("code", code)

	endpoint := "https://" + TokenHost + "/oauth/access_token"

	var out shortLivedResponse
	if err := s.postForm(ctx, endpoint, form, &out); err != nil {
		return nil, err
	}

	token, userID := out.AccessToken, out.UserID
	perms := out.Permissions.Strings()
	if len(out.Data) > 0 {
		token = out.Data[0].AccessToken
		userID = out.Data[0].UserID
		perms = out.Data[0].Permissions.Strings()
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("instagram: code exchange returned an empty access token")
	}

	return &igdomain.TokenGrant{
		AccessToken: token,
		// The short-lived token is valid for one hour; the response does not
		// carry expires_in.
		ExpiresIn:   time.Hour,
		UserID:      userID.String(),
		Permissions: perms,
	}, nil
}

// longLivedResponse is returned by both the long-lived exchange and the refresh.
type longLivedResponse struct {
	AccessToken string         `json:"access_token"`
	TokenType   string         `json:"token_type"`
	ExpiresIn   int64          `json:"expires_in"`
	Permissions permissionList `json:"permissions"`
}

func (s *oauthService) ExchangeForLongLived(ctx context.Context, shortLivedToken string) (*igdomain.TokenGrant, error) {
	q := url.Values{}
	q.Set("grant_type", "ig_exchange_token")
	q.Set("client_secret", s.cfg.AppSecret)
	q.Set("access_token", shortLivedToken)

	var out longLivedResponse
	if err := s.get(ctx, "https://"+GraphHost+"/access_token?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	if strings.TrimSpace(out.AccessToken) == "" {
		return nil, fmt.Errorf("instagram: long-lived exchange returned an empty access token")
	}
	return &igdomain.TokenGrant{
		AccessToken: out.AccessToken,
		ExpiresIn:   time.Duration(out.ExpiresIn) * time.Second,
		Permissions: out.Permissions.Strings(),
	}, nil
}

// RefreshToken extends a long-lived token.
//
// Deliberately does NOT send client_secret — unlike the long-lived exchange,
// this endpoint does not take it. Instagram also rejects a refresh on a token
// younger than 24 hours, which the caller enforces before calling.
func (s *oauthService) RefreshToken(ctx context.Context, longLivedToken string) (*igdomain.TokenGrant, error) {
	q := url.Values{}
	q.Set("grant_type", "ig_refresh_token")
	q.Set("access_token", longLivedToken)

	var out longLivedResponse
	if err := s.get(ctx, "https://"+GraphHost+"/refresh_access_token?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	if strings.TrimSpace(out.AccessToken) == "" {
		return nil, fmt.Errorf("instagram: token refresh returned an empty access token")
	}
	return &igdomain.TokenGrant{
		AccessToken: out.AccessToken,
		ExpiresIn:   time.Duration(out.ExpiresIn) * time.Second,
		Permissions: out.Permissions.Strings(),
	}, nil
}

// profileResponse is GET /me.
//
// UserID and ID are BOTH returned and they are DIFFERENT values: user_id is the
// Instagram professional account ID used in endpoint paths, while id is
// app-scoped and is not usable as <IG_ID>. Confusing them is the single most
// common Instagram Login mistake, so only UserID is propagated.
type profileResponse struct {
	UserID            graphID `json:"user_id"`
	ID                graphID `json:"id"`
	Username          string  `json:"username"`
	Name              string  `json:"name"`
	AccountType       string  `json:"account_type"`
	ProfilePictureURL string  `json:"profile_picture_url"`
	FollowersCount    int     `json:"followers_count"`
	FollowsCount      int     `json:"follows_count"`
	MediaCount        int     `json:"media_count"`
}

func (s *oauthService) GetProfile(ctx context.Context, token string) (*igdomain.Profile, error) {
	q := url.Values{}
	q.Set("fields", "user_id,username,name,account_type,profile_picture_url,followers_count,follows_count,media_count")
	q.Set("access_token", token)

	endpoint := "https://" + GraphHost + "/" + s.cfg.GraphVersion + "/me?" + q.Encode()

	var out profileResponse
	if err := s.get(ctx, endpoint, &out); err != nil {
		return nil, err
	}

	igUserID := out.UserID.String()
	if igUserID == "" {
		return nil, fmt.Errorf("instagram: GET /me did not return user_id (the app-scoped id must not be used as the account id)")
	}

	return &igdomain.Profile{
		IGUserID:          igUserID,
		Username:          out.Username,
		Name:              out.Name,
		AccountType:       out.AccountType,
		ProfilePictureURL: out.ProfilePictureURL,
		FollowersCount:    out.FollowersCount,
		FollowsCount:      out.FollowsCount,
		MediaCount:        out.MediaCount,
	}, nil
}

// ---------------------------------------------------------------- transport

func (s *oauthService) get(ctx context.Context, endpoint string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	return s.do(req, out)
}

func (s *oauthService) postForm(ctx context.Context, endpoint string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return s.do(req, out)
}

func (s *oauthService) do(req *http.Request, out any) error {
	started := time.Now()

	resp, err := s.http.Do(req)
	if err != nil {
		log.Printf("[instagram-oauth] %s %s transport error after %s: %v",
			req.Method, req.URL.Host+req.URL.Path, time.Since(started).Round(time.Millisecond), err)
		return fmt.Errorf("instagram oauth: %s: %w", req.URL.Path, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("instagram oauth: read body: %w", err)
	}

	log.Printf("[instagram-oauth] %s %s -> %d in %s (%d bytes)",
		req.Method, req.URL.Host+req.URL.Path, resp.StatusCode,
		time.Since(started).Round(time.Millisecond), len(raw))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Log the upstream body verbatim. Token responses only reach here on
		// FAILURE, so this carries Meta's error detail and no credential.
		log.Printf("[instagram-oauth] %s %s failed, body: %s",
			req.Method, req.URL.Host+req.URL.Path, truncateBody(raw))
		return parseOAuthError(resp.StatusCode, raw)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("instagram oauth: decode %s: %w", req.URL.Path, err)
	}
	return nil
}

// oauthError covers both Graph's {"error":{...}} envelope and the flat
// {"error_type","error_message"} form the Instagram OAuth hosts use.
type oauthError struct {
	Error struct {
		Message   string `json:"message"`
		Type      string `json:"type"`
		Code      int    `json:"code"`
		Subcode   int    `json:"error_subcode"`
		FBTraceID string `json:"fbtrace_id"`
	} `json:"error"`
	ErrorType    string `json:"error_type"`
	ErrorMessage string `json:"error_message"`
	Code         int    `json:"code"`
}

func parseOAuthError(status int, raw []byte) error {
	var e oauthError
	if err := json.Unmarshal(raw, &e); err == nil {
		if e.Error.Message != "" {
			return fmt.Errorf("instagram oauth: http=%d code=%d subcode=%d trace=%s: %s",
				status, e.Error.Code, e.Error.Subcode, e.Error.FBTraceID, e.Error.Message)
		}
		if e.ErrorMessage != "" {
			return fmt.Errorf("instagram oauth: http=%d type=%s: %s", status, e.ErrorType, e.ErrorMessage)
		}
	}
	body := string(raw)
	if len(body) > 512 {
		body = body[:512] + "…"
	}
	return fmt.Errorf("instagram oauth: http=%d: %s", status, body)
}

// truncateBody bounds an upstream error body for logging.
func truncateBody(raw []byte) string {
	const limit = 1024
	if len(raw) <= limit {
		return string(raw)
	}
	return string(raw[:limit]) + "…"
}
