package tiktok

import (
	"context"
	"net/url"
	"strings"

	ttdomain "vozko/domain/tiktok"
)

var profileFields = []string{"username", "display_name", "profile_image", "has_advanced_access", "is_business_account"}

func (c *Client) Profile(ctx context.Context, creds ttdomain.Credentials) (ttdomain.BusinessProfile, error) {
	var out struct {
		Username          string `json:"username"`
		DisplayName       string `json:"display_name"`
		ProfileImage      string `json:"profile_image"`
		HasAdvancedAccess bool   `json:"has_advanced_access"`
		IsBusinessAccount bool   `json:"is_business_account"`
	}
	q := url.Values{}
	q.Set("business_id", creds.OpenID)
	q.Set("fields", jsonList(profileFields))
	if err := c.getJSON(ctx, request{path: "/business/get/", query: q, family: FamilyProfile, creds: &creds}, &out); err != nil {
		return ttdomain.BusinessProfile{}, err
	}
	return ttdomain.BusinessProfile{
		Username:          strings.TrimSpace(out.Username),
		DisplayName:       strings.TrimSpace(out.DisplayName),
		AvatarURL:         strings.TrimSpace(out.ProfileImage),
		HasAdvancedAccess: out.HasAdvancedAccess,
		IsBusinessAccount: out.IsBusinessAccount,
	}, nil
}
