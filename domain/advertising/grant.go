package advertising

import (
	"fmt"
	"slices"
	"sort"
	"time"

	"vozko/domain/facebook"
)

const (
	ScopeAdsManagement      = "ads_management"
	ScopeAdsRead            = "ads_read"
	ScopeBusinessManagement = "business_management"
	ScopePagesManageAds     = "pages_manage_ads"
	ScopePagesShowList      = "pages_show_list"
	ScopePagesReadEngage    = "pages_read_engagement"
)

func RequiredScopes() []string {
	return []string{
		ScopeAdsManagement, ScopeAdsRead, ScopeBusinessManagement,
		ScopePagesManageAds, ScopePagesShowList, ScopePagesReadEngage,
	}
}

type GrantStatus string

const (
	GrantActive  GrantStatus = "ACTIVE"
	GrantRevoked GrantStatus = "REVOKED"
)

type Grant struct {
	ID               string
	WorkspaceID      string
	ConnectedBy      string
	TokenKind        facebook.TokenKind
	AccessToken      string
	TokenExpiresAt   *time.Time
	AppScopedUserID  string
	ClientBusinessID string
	Scopes           []string
	GranularScopes   map[string][]string
	Status           GrantStatus
	CheckedAt        *time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

func (g *Grant) MissingScopes() []string {
	var missing []string
	for _, scope := range RequiredScopes() {
		if g == nil || !slices.Contains(g.Scopes, scope) {
			missing = append(missing, scope)
		}
	}
	sort.Strings(missing)
	return missing
}

func (g *Grant) Allows(scope, targetID string) bool {
	if g == nil || g.Status != GrantActive || g.AccessToken == "" || !slices.Contains(g.Scopes, scope) {
		return false
	}
	targets, restricted := g.GranularScopes[scope]
	if !restricted || len(targets) == 0 {
		return true
	}
	return slices.Contains(targets, targetID)
}

func (g *Grant) Usable(now time.Time) error {
	if g == nil || g.Status != GrantActive || g.AccessToken == "" {
		return ErrAccountNeedsReconnect
	}
	if g.TokenExpiresAt != nil && !now.Before(*g.TokenExpiresAt) {
		return fmt.Errorf("%w: token expired at %s", ErrAccountNeedsReconnect, g.TokenExpiresAt.Format(time.RFC3339))
	}
	if missing := g.MissingScopes(); len(missing) > 0 {
		return fmt.Errorf("%w: %v", ErrMissingScopes, missing)
	}
	return nil
}

const (
	OAuthStartPath    = "/oauth/meta-ads/start"
	OAuthCallbackPath = "/oauth/meta-ads/callback"
	WebhookPath       = "/webhooks/meta-ads"
)
