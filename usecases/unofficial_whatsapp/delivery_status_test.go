package unofficial_whatsapp

import (
	"testing"

	"vozko/domain/conversation"
	uw "vozko/domain/unofficial_whatsapp"
)

func TestDeliveryStatusMapsToTheCRMTicks(t *testing.T) {
	cases := []struct {
		provider uw.DeliveryStatus
		want     conversation.DeliveryStatus
	}{
		{uw.DeliveryQueued, conversation.DeliveryStatusSent},
		{uw.DeliverySent, conversation.DeliveryStatusSent},
		{uw.DeliveryDelivered, conversation.DeliveryStatusDelivered},
		{uw.DeliveryRead, conversation.DeliveryStatusRead},
		{uw.DeliveryFailed, conversation.DeliveryStatusFailed},
		{uw.DeliveryUnknown, conversation.DeliveryStatusNone},
		{uw.DeliveryDeleted, conversation.DeliveryStatusNone},
	}

	for _, tc := range cases {
		t.Run(string(tc.provider), func(t *testing.T) {
			if got := crmDeliveryStatus(tc.provider); got != tc.want {
				t.Errorf("crmDeliveryStatus(%q) = %q, want %q", tc.provider, got, tc.want)
			}
		})
	}
}

func TestStatusUpdateSurvivesAnUnresolvableChat(t *testing.T) {
	for _, status := range []uw.DeliveryStatus{
		uw.DeliveryDelivered, uw.DeliveryRead, uw.DeliveryFailed,
	} {
		if got := crmDeliveryStatus(status); got == "" {
			t.Errorf("%q maps to nothing; a receipt with no chat id would write nothing", status)
		}
	}
}
