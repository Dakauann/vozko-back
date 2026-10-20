package tiktok

import (
	"context"
	"net/url"
	"strings"

	ttdomain "vozko/domain/tiktok"
)

func (c *Client) Subscription(ctx context.Context, kind ttdomain.WebhookKind) (*ttdomain.WebhookSubscription, error) {
	q := url.Values{}
	q.Set("app_id", c.appID)
	q.Set("secret", c.appSecret)
	q.Set("event_type", string(kind))
	var out struct {
		CallbackURL string   `json:"callback_url"`
		ItemList    []string `json:"item_list"`
	}
	if err := c.getJSON(ctx, request{path: "/business/webhook/list/", query: q, family: FamilyWebhookConfig}, &out); err != nil {
		return nil, err
	}
	if strings.TrimSpace(out.CallbackURL) == "" {
		return nil, nil
	}
	return &ttdomain.WebhookSubscription{Kind: kind, CallbackURL: strings.TrimSpace(out.CallbackURL), ItemFilter: out.ItemList}, nil
}

func (c *Client) Subscribe(ctx context.Context, kind ttdomain.WebhookKind, callbackURL string) error {
	return c.postJSON(ctx, request{path: "/business/webhook/update/", family: FamilyWebhookConfig, retrySafe: true}, map[string]string{
		"app_id":       c.appID,
		"secret":       c.appSecret,
		"event_type":   string(kind),
		"callback_url": callbackURL,
	}, nil)
}

var (
	_ ttdomain.OAuthService         = (*Client)(nil)
	_ ttdomain.ProfileService       = (*Client)(nil)
	_ ttdomain.MessagingService     = (*Client)(nil)
	_ ttdomain.VideoService         = (*Client)(nil)
	_ ttdomain.CommentService       = (*Client)(nil)
	_ ttdomain.WebhookConfigService = (*Client)(nil)
)
