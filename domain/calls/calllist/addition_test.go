package calllist

import (
	"errors"
	"fmt"
	"testing"

	"vozko/domain/lead"
)

func TestAListTakesNewLeadsOnlyWhileItIsWorkedAndHasRoom(t *testing.T) {
	cases := []struct {
		status Status
		count  int
		want   error
	}{
		{StatusActive, 0, nil},
		{StatusPaused, 10, nil},
		{StatusActive, MaxItems - 1, nil},
		{StatusActive, MaxItems, ErrListFull},
		{StatusBuilding, 0, ErrListBuilding},
		{StatusArchived, 0, ErrListNotActive},
		{StatusFailed, 0, ErrListNotActive},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s with %d", tc.status, tc.count), func(t *testing.T) {
			l := &List{Status: tc.status, ItemCount: tc.count}
			if err := l.AcceptsNewLeads(); !errors.Is(err, tc.want) {
				t.Fatalf("AcceptsNewLeads() = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestARefusedAdmissionNamesItsReasonAndCode(t *testing.T) {
	err := fmt.Errorf("add: %w", AdmissionRefused(SkipReason(lead.DialRefusedOptedOut)))
	if !errors.Is(err, ErrLeadNotAdmitted) {
		t.Fatalf("a refused admission must be ErrLeadNotAdmitted, got %v", err)
	}
	var refusal *AdmissionRefusal
	if !errors.As(err, &refusal) || refusal.Reason != SkipReason(lead.DialRefusedOptedOut) {
		t.Fatalf("the refusal must carry its reason, got %+v", refusal)
	}
	if got := ErrorCode(err); got != "call_list_lead_not_admitted" {
		t.Fatalf("code = %q", got)
	}
	if got := ErrorCode(ErrListFull); got != "call_list_full" {
		t.Fatalf("code = %q", got)
	}
}
