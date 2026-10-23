package carriercampaign

import (
	"errors"
	"testing"

	"vozko/domain/crmfilter"
	"vozko/domain/selection"
)

func vipFilter() *crmfilter.Filter {
	return &crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{{Field: crmfilter.FieldLabel, Operator: crmfilter.OpIn, Values: []string{"vip"}}}}}}
}

func TestConfirmAudience(t *testing.T) {
	vip := vipFilter()
	vipPrint := selection.Fingerprint(*vip)
	everyonePrint := selection.Fingerprint(crmfilter.Filter{})
	cases := []struct {
		name     string
		audience selection.Selection
		matched  int
		want     error
	}{
		{"picked leads need no count", selection.Selection{IDs: []string{"l1", "l2"}}, 2, nil},
		{"everyone confirmed", selection.Selection{Mode: selection.ModeEveryone, Filter: &crmfilter.Filter{}, ExpectedCount: 40, Fingerprint: everyonePrint}, 40, nil},
		{"everyone never counted", selection.Selection{Mode: selection.ModeEveryone, Filter: &crmfilter.Filter{}}, 40, selection.ErrEveryoneUnconfirmed},
		{"everyone grew", selection.Selection{Mode: selection.ModeEveryone, Filter: &crmfilter.Filter{}, ExpectedCount: 40, Fingerprint: everyonePrint}, 41, selection.ErrCountChanged},
		{"filter confirmed", selection.Selection{Filter: vip, ExpectedCount: 7, Fingerprint: vipPrint}, 7, nil},
		{"filter counted on another filter", selection.Selection{Filter: vip, ExpectedCount: 7, Fingerprint: everyonePrint}, 7, selection.ErrFingerprintMismatch},
		{"filter without a count", selection.Selection{Filter: vip, Fingerprint: vipPrint}, 7, selection.ErrCountRequired},
		{"filter shrank", selection.Selection{Filter: vip, ExpectedCount: 7, Fingerprint: vipPrint}, 5, selection.ErrCountChanged},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := Campaign{Audience: tc.audience}
			if err := c.ConfirmAudience(tc.matched); !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
				t.Fatalf("ConfirmAudience = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestAudienceFingerprint(t *testing.T) {
	vip := vipFilter()
	if got := (Campaign{Audience: selection.Selection{Filter: vip}}).AudienceFingerprint(); got != selection.Fingerprint(*vip) {
		t.Fatalf("filter fingerprint = %q", got)
	}
	if got := (Campaign{Audience: selection.Selection{IDs: []string{"l1"}}}).AudienceFingerprint(); got != "" {
		t.Fatalf("picked leads have no fingerprint, got %q", got)
	}
	if got := (Campaign{}).AudienceFingerprint(); got != "" {
		t.Fatalf("no audience has no fingerprint, got %q", got)
	}
}
