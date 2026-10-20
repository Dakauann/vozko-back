package tiktok

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"

	ttdomain "vozko/domain/tiktok"
)

type WebhookSubscriptions struct {
	webhooks    ttdomain.WebhookConfigService
	callbackURL string
	metrics     Metrics
}

func NewWebhookSubscriptions(webhooks ttdomain.WebhookConfigService, callbackURL string, metrics Metrics) *WebhookSubscriptions {
	if webhooks == nil || metrics == nil {
		panic("tiktok: webhook subscriptions require the webhook config service and metrics")
	}
	if err := ValidateCallbackURL(callbackURL); err != nil {
		panic(err.Error())
	}
	return &WebhookSubscriptions{webhooks: webhooks, callbackURL: strings.TrimSpace(callbackURL), metrics: metrics}
}

func ValidateCallbackURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("tiktok: TIKTOK_WEBHOOK_URL must be an absolute https URL without query or fragment, got %q", raw)
	}
	return nil
}

func (w *WebhookSubscriptions) Ensure(ctx context.Context) (bool, error) {
	healthy := true
	for _, kind := range ttdomain.WebhookKinds() {
		sub, err := w.webhooks.Subscription(ctx, kind)
		if err != nil {
			w.metrics.AddTikTokEvent("webhook_config", "unreadable")
			return false, fmt.Errorf("tiktok: read the %s webhook configuration: %w", kind, err)
		}
		if sub != nil && len(sub.ItemFilter) > 0 {
			w.metrics.AddTikTokEvent("webhook_config", "item_filter")
			log.Printf("[tiktok][webhooks] the %s subscription filters %d post(s); every other post is silent until an operator deletes the filter", kind, len(sub.ItemFilter))
			healthy = false
			continue
		}
		if sub != nil && sub.CallbackURL == w.callbackURL {
			continue
		}
		if err := w.webhooks.Subscribe(ctx, kind, w.callbackURL); err != nil {
			w.metrics.AddTikTokEvent("webhook_config", "subscribe_failed")
			return false, fmt.Errorf("tiktok: subscribe the %s webhook: %w", kind, err)
		}
		w.metrics.AddTikTokEvent("webhook_config", "subscribed")
	}
	return healthy, nil
}
