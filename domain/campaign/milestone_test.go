package campaign

import (
	"reflect"
	"testing"
)

func TestMilestonesNameEveryMomentAStatusProves(t *testing.T) {
	cases := []struct {
		name   string
		status SendStatus
		want   []Milestone
	}{
		{"a sent message was only sent", SendStatusSent, []Milestone{MilestoneSent}},
		{"a delivery proves the send", SendStatusDelivered, []Milestone{MilestoneSent, MilestoneDelivered}},
		{"Meta can report read without a delivered receipt, and a read message was delivered", SendStatusRead, []Milestone{MilestoneSent, MilestoneDelivered, MilestoneRead}},
		{"a failure proves nothing about an earlier send", SendStatusFailed, []Milestone{MilestoneFailed}},
		{"pending has not happened yet", SendStatusPending, nil},
		{"a spam gate is a decision, not a delivery moment", SendStatusNotEligiblePossibleSpam, nil},
		{"a number off WhatsApp was never sent", SendStatusSkippedNotOnWhatsApp, nil},
		{"an unknown status stamps nothing", SendStatus("SOMETHING_ELSE"), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.status.Milestones(); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Milestones(%s) = %v, want %v", tc.status, got, tc.want)
			}
		})
	}
}

func TestAllMilestonesCoversEveryStampedMoment(t *testing.T) {
	all := make(map[Milestone]bool)
	for _, m := range AllMilestones() {
		all[m] = true
	}
	for _, s := range []SendStatus{
		SendStatusPending, SendStatusSent, SendStatusDelivered, SendStatusRead,
		SendStatusFailed, SendStatusNotEligiblePossibleSpam, SendStatusSkippedNotOnWhatsApp,
	} {
		for _, m := range s.Milestones() {
			if !all[m] {
				t.Errorf("status %s stamps %s, which AllMilestones does not list, so a reset would leave it behind", s, m)
			}
		}
	}
}
