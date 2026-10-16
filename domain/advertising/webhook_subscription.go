package advertising

import (
	"errors"
	"slices"
	"strings"

	"vozko/domain/metaplatform"
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

type AppSubscription = metaplatform.AppSubscription

func AccountWebhook(callbackURL string) AppSubscription {
	return AppSubscription{Object: accountWebhookObject, CallbackURL: strings.TrimSpace(callbackURL), Fields: slices.Clone(accountWebhookFields), Active: true}
}
