package facebook

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	fbdomain "vozko/domain/facebook"
	"vozko/infra/meta"
)

const (
	DialogHost = "www.facebook.com"
	GraphHost  = "graph.facebook.com"
)

const pageFields = "id,name,username,category,link,access_token,tasks,picture{url},followers_count,instagram_business_account"

type OAuthConfig struct {
	AppID        string
	AppSecret    string
	ConfigID     string
	RedirectURI  string
	GraphVersion string
	HTTPClient   *http.Client
}

type oauthService struct {
	cfg    OAuthConfig
	client *meta.Client
}

func NewOAuthService(cfg OAuthConfig) (fbdomain.OAuthService, error) {
	if strings.TrimSpace(cfg.AppID) == "" || strings.TrimSpace(cfg.AppSecret) == "" {
		return nil, fmt.Errorf("facebook: app id and secret are required")
	}
	if strings.TrimSpace(cfg.ConfigID) == "" {
		return nil, fmt.Errorf("facebook: a Facebook Login for Business configuration id is required")
	}
	if err := fbdomain.ValidateRedirectURI(cfg.RedirectURI); err != nil {
		return nil, err
	}
	cfg.GraphVersion = meta.VersionOr(cfg.GraphVersion)
	client, err := meta.NewClient(meta.Config{
		Host:       GraphHost,
		APIVersion: cfg.GraphVersion,
		AppSecret:  cfg.AppSecret,
		HTTPClient: cfg.HTTPClient,
	})
	if err != nil {
		return nil, err
	}
	return &oauthService{cfg: cfg, client: client}, nil
}

func (s *oauthService) BuildAuthorizeURL(state string) string {
	q := url.Values{}
	q.Set("client_id", s.cfg.AppID)
	q.Set("redirect_uri", s.cfg.RedirectURI)
	q.Set("state", state)
	q.Set("config_id", s.cfg.ConfigID)
	q.Set("response_type", "code")
	q.Set("override_default_response_type", "true")
	return "https://" + DialogHost + "/" + s.cfg.GraphVersion + "/dialog/oauth?" + q.Encode()
}

type codeExchangeResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
}

func (s *oauthService) ExchangeCode(ctx context.Context, code string) (*fbdomain.TokenGrant, error) {
	q := url.Values{}
	q.Set("client_id", s.cfg.AppID)
	q.Set("client_secret", s.cfg.AppSecret)
	q.Set("redirect_uri", s.cfg.RedirectURI)
	q.Set("code", code)

	var out codeExchangeResponse
	if err := s.client.Do(ctx, meta.Request{Method: http.MethodGet, Path: "/oauth/access_token", Query: q}, &out); err != nil {
		return nil, err
	}
	if strings.TrimSpace(out.AccessToken) == "" {
		return nil, fmt.Errorf("facebook: code exchange returned an empty access token")
	}
	grant := &fbdomain.TokenGrant{AccessToken: out.AccessToken}
	if out.ExpiresIn > 0 {
		at := time.Now().UTC().Add(time.Duration(out.ExpiresIn) * time.Second)
		grant.ExpiresAt = &at
	}
	return grant, nil
}

type debugTokenResponse struct {
	Data struct {
		IsValid        bool                `json:"is_valid"`
		Type           string              `json:"type"`
		UserID         meta.GraphID        `json:"user_id"`
		ExpiresAt      int64               `json:"expires_at"`
		Scopes         meta.PermissionList `json:"scopes"`
		GranularScopes []struct {
			Scope     string         `json:"scope"`
			TargetIDs []meta.GraphID `json:"target_ids"`
		} `json:"granular_scopes"`
	} `json:"data"`
}

func (s *oauthService) DebugToken(ctx context.Context, token string) (*fbdomain.TokenDebug, error) {
	q := url.Values{}
	q.Set("input_token", token)
	q.Set("access_token", s.cfg.AppID+"|"+s.cfg.AppSecret)

	var out debugTokenResponse
	if err := s.client.Do(ctx, meta.Request{Method: http.MethodGet, Path: "/debug_token", Query: q}, &out); err != nil {
		return nil, err
	}
	d := out.Data
	debug := &fbdomain.TokenDebug{
		Valid:          d.IsValid,
		Type:           d.Type,
		AppScopedUser:  d.UserID.String(),
		Scopes:         d.Scopes.Strings(),
		GranularScopes: make(map[string][]string, len(d.GranularScopes)),
	}
	if d.ExpiresAt > 0 {
		at := time.Unix(d.ExpiresAt, 0).UTC()
		debug.ExpiresAt = &at
	}
	for _, g := range d.GranularScopes {
		var targets []string
		for _, id := range g.TargetIDs {
			targets = append(targets, id.String())
		}
		debug.GranularScopes[g.Scope] = targets
	}
	return debug, nil
}

type identityResponse struct {
	ID               meta.GraphID `json:"id"`
	Name             string       `json:"name"`
	ClientBusinessID meta.GraphID `json:"client_business_id"`
}

func (s *oauthService) Identify(ctx context.Context, token string) (*fbdomain.GrantIdentity, error) {
	q := url.Values{}
	q.Set("fields", "id,name,client_business_id")
	var out identityResponse
	if err := s.client.Do(ctx, meta.Request{Method: http.MethodGet, Path: "/me", Token: token, Query: q}, &out); err != nil {
		return nil, err
	}
	if out.ID.String() == "" {
		return nil, fmt.Errorf("facebook: /me did not return an id")
	}
	return &fbdomain.GrantIdentity{
		AppScopedUserID:  out.ID.String(),
		Name:             out.Name,
		ClientBusinessID: out.ClientBusinessID.String(),
	}, nil
}

type remotePage struct {
	ID          meta.GraphID `json:"id"`
	Name        string       `json:"name"`
	Username    string       `json:"username"`
	Category    string       `json:"category"`
	Link        string       `json:"link"`
	AccessToken string       `json:"access_token"`
	Tasks       []string     `json:"tasks"`
	Picture     struct {
		Data struct {
			URL string `json:"url"`
		} `json:"data"`
	} `json:"picture"`
	FollowersCount   int `json:"followers_count"`
	InstagramAccount *struct {
		ID meta.GraphID `json:"id"`
	} `json:"instagram_business_account"`
}

func (p remotePage) toDomain() *fbdomain.RemotePage {
	out := &fbdomain.RemotePage{
		FBPageID:       p.ID.String(),
		Name:           p.Name,
		Username:       p.Username,
		Category:       p.Category,
		Link:           p.Link,
		AccessToken:    p.AccessToken,
		PictureURL:     p.Picture.Data.URL,
		FollowersCount: p.FollowersCount,
	}
	for _, t := range p.Tasks {
		out.Tasks = append(out.Tasks, fbdomain.Task(strings.ToUpper(strings.TrimSpace(t))))
	}
	if p.InstagramAccount != nil {
		out.LinkedIGUserID = p.InstagramAccount.ID.String()
	}
	return out
}

type pageListResponse struct {
	Data   []remotePage `json:"data"`
	Paging struct {
		Cursors struct {
			After string `json:"after"`
		} `json:"cursors"`
		Next string `json:"next"`
	} `json:"paging"`
}

const maxPageListRequests = 50

func (s *oauthService) ListPages(ctx context.Context, token string) ([]*fbdomain.RemotePage, error) {
	var pages []*fbdomain.RemotePage
	after := ""
	for i := 0; i < maxPageListRequests; i++ {
		q := url.Values{}
		q.Set("fields", pageFields)
		q.Set("limit", "100")
		if after != "" {
			q.Set("after", after)
		}
		var out pageListResponse
		if err := s.client.Do(ctx, meta.Request{Method: http.MethodGet, Path: "/me/accounts", Token: token, Query: q}, &out); err != nil {
			return nil, err
		}
		for _, p := range out.Data {
			pages = append(pages, p.toDomain())
		}
		if out.Paging.Next == "" || out.Paging.Cursors.After == "" {
			return pages, nil
		}
		after = out.Paging.Cursors.After
	}
	return nil, fmt.Errorf("facebook: /me/accounts did not finish paging after %d requests", maxPageListRequests)
}

func (s *oauthService) GetPage(ctx context.Context, pageToken, fbPageID string) (*fbdomain.RemotePage, error) {
	q := url.Values{}
	q.Set("fields", "id,name,username,category,link,picture{url},followers_count,instagram_business_account")
	var out remotePage
	if err := s.client.Do(ctx, meta.Request{Method: http.MethodGet, Path: "/" + fbPageID, Token: pageToken, Query: q}, &out); err != nil {
		return nil, err
	}
	return out.toDomain(), nil
}
