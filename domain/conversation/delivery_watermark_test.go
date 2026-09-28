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
