package main

import (
	"reflect"
	"testing"
	"time"

	"vozko/domain/carriermessaging"
	"vozko/infra/cmcom"
)

func TestLifecycle(t *testing.T) {
	accepted := step{After: acceptedAfter, Status: carriermessaging.StatusAccepted, Description: "Accepted"}
	delivered := step{After: deliveredAfter, Status: carriermessaging.StatusDelivered, Description: "Delivered"}
	read := step{After: readAfter, Status: carriermessaging.StatusRead, Description: "Read"}
	noRCS := step{After: deliveredAfter, Status: carriermessaging.StatusFailed, ErrorCode: cmcom.UnreachableErrorCode, Description: unreachableDescription}
	cases := []struct {
		name    string
		channel string
		phone   string
		want    []step
	}{
		{"rcs delivered and read", channelRCS, "5511987654320", []step{accepted, delivered, read}},
		{"rcs device without rcs", channelRCS, "5511987654321", []step{accepted, noRCS}},
		{"rcs never reported", channelRCS, "5511987654322", nil},
		{"rcs other endings land", channelRCS, "5511987654327", []step{accepted, delivered, read}},
		{"sms stops at delivered", channelSMS, "5511987654320", []step{accepted, delivered}},
		{"sms reaches a phone without rcs", channelSMS, "5511987654321", []step{accepted, delivered}},
		{"sms never reported", channelSMS, "5511987654322", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := lifecycle(tc.channel, tc.phone); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("lifecycle = %+v\nwant %+v", got, tc.want)
			}
		})
	}
	if acceptedAfter >= deliveredAfter || deliveredAfter != 2*time.Second || readAfter != 5*time.Second {
		t.Fatalf("timings = %s %s %s", acceptedAfter, deliveredAfter, readAfter)
	}
}

func TestOutcomeForRejectsPhonesEndingInThree(t *testing.T) {
	for _, phone := range []string{"5511987654320", "5511987654321", "5511987654322", "5511987654329"} {
		if outcomeFor(phone) != outcomeFor("5511987654320") {
			t.Fatalf("%s must be accepted", phone)
		}
	}
	if outcomeFor("5511987654323") == outcomeFor("5511987654320") {
		t.Fatalf("phones ending in 3 must be rejected")
	}
}
