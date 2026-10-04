package advertising

import (
	"errors"
	"slices"
	"strings"
)

var ErrWebhookNotSubscribed = errors.New("meta did not keep the ad account webhook subscription")

const accountWebhookObject = "ad_account"

var accountWebhookFields = []string{
	"effective_status",
	"in_process_ad_objects",
	"with_issues_ad_objects",
	"ad_recommendations",
	"creative_fatigue",
	"product_set_issue",
	"ads_async_creation_request",
	"marketing_messages_subscriber_upload_status",
}

type AppSubscription struct {
	Object      string
	CallbackURL string
	Fields      []string
	Active      bool
}

func AccountWebhook(callbackURL string) AppSubscription {
	return AppSubscription{Object: accountWebhookObject, CallbackURL: strings.TrimSpace(callbackURL), Fields: slices.Clone(accountWebhookFields), Active: true}
}

func (s AppSubscription) CoveredBy(current []AppSubscription) bool {
	for _, c := range current {
		if c.Object != s.Object || !c.Active || c.CallbackURL != s.CallbackURL {
			continue
		}
		for _, field := range s.Fields {
			if !slices.Contains(c.Fields, field) {
				return false
			}
		}
		return true
	}
	return false
}
