package conversation

import (
	"reflect"
	"testing"
)

func TestDeliveryStatusOnlyMovesForward(t *testing.T) {
	cases := map[DeliveryStatus][]DeliveryStatus{
		DeliveryStatusDelivered: {DeliveryStatusNone, DeliveryStatusSent},
		DeliveryStatusRead:      {DeliveryStatusNone, DeliveryStatusSent, DeliveryStatusDelivered},
		DeliveryStatusSent:      nil,
		DeliveryStatusFailed:    nil,
	}
	for status, want := range cases {
		if got := status.Supersedes(); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s supersedes %v, want %v", status, got, want)
		}
	}
}

func TestOneMessageReceiptReplacesOnlyEarlierStatuses(t *testing.T) {
	all := []DeliveryStatus{DeliveryStatusNone, DeliveryStatusSent, DeliveryStatusDelivered, DeliveryStatusRead, DeliveryStatusFailed}
	cases := map[DeliveryStatus][]DeliveryStatus{
		DeliveryStatusNone:      nil,
		DeliveryStatusSent:      nil,
		DeliveryStatusDelivered: {DeliveryStatusNone, DeliveryStatusSent},
		DeliveryStatusRead:      {DeliveryStatusNone, DeliveryStatusSent, DeliveryStatusDelivered},
		DeliveryStatusFailed:    {DeliveryStatusNone, DeliveryStatusSent, DeliveryStatusDelivered, DeliveryStatusRead},
	}
	for next, want := range cases {
		if got := next.Replaces(); !reflect.DeepEqual(got, want) {
			t.Fatalf("%q replaces %v, want %v", next, got, want)
		}
		for _, current := range all {
			allowed := false
			for _, w := range want {
				allowed = allowed || w == current
			}
			if got := next.Advances(current); got != allowed {
				t.Fatalf("%q advances %q = %v, want %v", next, current, got, allowed)
			}
		}
	}
}
