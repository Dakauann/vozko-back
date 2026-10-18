package campaign

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/lead"
	wo "vozko/domain/whatsapp_outreach"
)

func TestARecipientRefusalCarriesTheOutreachCode(t *testing.T) {
	opted := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		contact *lead.Lead
		want    error
		code    string
	}{
		{"a reachable lead", &lead.Lead{Number: "5511987654321"}, nil, ""},
		{"an opted-out lead", &lead.Lead{Number: "5511987654321", OptedOutAt: &opted}, wo.ErrLeadOptedOut, "lead_opted_out"},
		{"a blocked lead", &lead.Lead{Number: "5511987654321", Blocked: true}, wo.ErrLeadBlocked, "lead_blocked"},
		{"a lead without a number", &lead.Lead{Name: "Ana"}, wo.ErrInvalidPhone, "invalid_phone"},
		{"no lead at all", nil, wo.ErrInvalidPhone, "invalid_phone"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := RecipientRefusal(tc.contact)
			if !errors.Is(err, tc.want) || (tc.want == nil && err != nil) || wo.ErrorCode(err) != tc.code {
				t.Fatalf("RecipientRefusal = %v (code %q), want %v (%q)", err, wo.ErrorCode(err), tc.want, tc.code)
			}
		})
	}
}

func TestAChannelContactIsReachedByItsChannelNotByANumber(t *testing.T) {
	opted := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	if err := ContactRefusal(&lead.Lead{Name: "Ana"}); err != nil {
		t.Fatalf("an Instagram lead without a number = %v, want reachable", err)
	}
	if err := ContactRefusal(&lead.Lead{Name: "Ana", OptedOutAt: &opted}); !errors.Is(err, wo.ErrLeadOptedOut) {
		t.Fatalf("an opted-out channel lead = %v, want ErrLeadOptedOut", err)
	}
	if err := ContactRefusal(&lead.Lead{Name: "Ana", Blocked: true}); !errors.Is(err, wo.ErrLeadBlocked) {
		t.Fatalf("a blocked channel lead = %v, want ErrLeadBlocked", err)
	}
	if err := ContactRefusal(nil); err != nil {
		t.Fatalf("a conversation without a lead = %v, want reachable", err)
	}
}
