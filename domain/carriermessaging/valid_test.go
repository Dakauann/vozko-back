package carriermessaging

import "testing"

func TestEnumsKnowTheirValues(t *testing.T) {
	cases := []struct {
		name  string
		valid func() bool
		want  bool
	}{
		{"step rcs", StepRCS.Valid, true},
		{"step whatsapp", StepWhatsApp.Valid, true},
		{"step sms", StepSMS.Valid, true},
		{"step empty", Step("").Valid, false},
		{"step unknown", Step("email").Valid, false},
		{"run campaign", RunCampaign.Valid, true},
		{"run automation", RunAutomation.Valid, true},
		{"run operator", RunOperator.Valid, true},
		{"run workflow", RunWorkflow.Valid, true},
		{"run test", RunTest.Valid, true},
		{"run empty", RunKind("").Valid, false},
		{"run unknown", RunKind("blast").Valid, false},
		{"event inbound", EventInbound.Valid, true},
		{"event tap", EventTap.Valid, true},
		{"event click", EventClick.Valid, true},
		{"event opt out", EventOptOut.Valid, true},
		{"event unknown", EventKind("like").Valid, false},
		{"source keyword", SuppressionKeyword.Valid, true},
		{"source button", SuppressionButton.Valid, true},
		{"source operator", SuppressionOperator.Valid, true},
		{"source empty", SuppressionSource("").Valid, false},
		{"source unknown", SuppressionSource("rumour").Valid, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.valid(); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEveryStatusOfTheLifecycleIsValid(t *testing.T) {
	for _, s := range AllStatuses() {
		if !s.Valid() {
			t.Fatalf("%s is listed but not valid", s)
		}
	}
	if len(AllStatuses()) != 7 {
		t.Fatalf("got %d statuses", len(AllStatuses()))
	}
}
