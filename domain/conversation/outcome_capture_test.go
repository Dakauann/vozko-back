package conversation

import (
	"errors"
	"testing"
	"time"
)

func captureFixture() *OutcomeCapture {
	enabled := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	capture := &OutcomeCapture{
		Enabled:          true,
		EnabledAt:        &enabled,
		RequireOnFinish:  true,
		DurableThreshold: 30,
		Outcomes: []Outcome{
			{Code: "sale", Label: "Venda fechada", IsDurable: true, Position: 1},
			{Code: "no_answer", Label: "Sem resposta", Position: 2},
		},
	}
	capture.Normalize()
	return capture
}

func TestOutcomeResolveTable(t *testing.T) {
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	on := captureFixture()
	var off *OutcomeCapture

	cases := []struct {
		name    string
		capture *OutcomeCapture
		source  CloseSource
		reason  CloseReason
		code    string
		want    string
		wantErr error
	}{
		{"human, capture off", off, CloseSourceHuman, CloseReasonManual, "", "", nil},
		{"human, capture off, code ignored", off, CloseSourceHuman, CloseReasonManual, "sale", "", nil},
		{"human, code given", on, CloseSourceHuman, CloseReasonManual, "sale", "sale", nil},
		{"human, code missing", on, CloseSourceHuman, CloseReasonManual, "", "", ErrOutcomeRequired},
		{"human, code unknown", on, CloseSourceHuman, CloseReasonManual, "invented", "", ErrOutcomeUnknown},

		{"ai, capture off", off, CloseSourceAI, CloseReasonAIResolved, "", "", nil},
		{"ai, code given", on, CloseSourceAI, CloseReasonAIResolved, "sale", "sale", nil},
		{"ai, code missing", on, CloseSourceAI, CloseReasonAIResolved, "", "", ErrOutcomeRequired},
		{"ai, code unknown", on, CloseSourceAI, CloseReasonAIResolved, "invented", "", ErrOutcomeUnknown},

		{"system, capture off", off, CloseSourceSystem, CloseReasonCustomerIdle, "", "", nil},
		{"system, code ignored", on, CloseSourceSystem, CloseReasonCustomerIdle, "sale", OutcomeSystemAutoClose, nil},
		{"system, code missing", on, CloseSourceSystem, CloseReasonMaxAge, "", OutcomeSystemAutoClose, nil},

		{"workflow, code given", on, CloseSourceSystem, CloseReasonWorkflow, "sale", "sale", nil},
		{"workflow, code missing", on, CloseSourceSystem, CloseReasonWorkflow, "", "", ErrOutcomeRequired},
		{"workflow, code unknown", on, CloseSourceSystem, CloseReasonWorkflow, "invented", "", ErrOutcomeUnknown},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.capture.Resolve(tc.source, tc.reason, tc.code, "", now)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("Resolve() err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Resolve() err = %v, want nil", err)
			}
			if got != tc.want {
				t.Fatalf("Resolve() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOutcomeResolveWithoutRequireOnFinishLetsAHumanCloseUncaptured(t *testing.T) {
	capture := captureFixture()
	capture.RequireOnFinish = false
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)

	got, err := capture.Resolve(CloseSourceHuman, CloseReasonManual, "", "", now)
	if err != nil {
		t.Fatalf("Resolve() err = %v, want nil", err)
	}
	if got != "" {
		t.Fatalf("Resolve() = %q, want an uncaptured close", got)
	}
}

// An agent or a workflow finishing a conversation follows the rule a person
// does. Only when the workspace does not require an outcome does a close without
// one get the reserved "unspecified" code, kept apart from the catalogue.
func TestOutcomeResolveWithoutRequireOnFinishMarksAutomationClosesUnspecified(t *testing.T) {
	capture := captureFixture()
	capture.RequireOnFinish = false
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		source CloseSource
		reason CloseReason
		want   string
	}{
		{CloseSourceAI, CloseReasonAIResolved, OutcomeAIUnspecified},
		{CloseSourceSystem, CloseReasonWorkflow, OutcomeWorkflowUnspecified},
	} {
		got, err := capture.Resolve(tc.source, tc.reason, "", "", now)
		if err != nil {
			t.Fatalf("Resolve(%s/%s) err = %v, want nil", tc.source, tc.reason, err)
		}
		if got != tc.want {
			t.Fatalf("Resolve(%s/%s) = %q, want %q", tc.source, tc.reason, got, tc.want)
		}
	}
}

func TestAppliesToRespectsEnabledAt(t *testing.T) {
	capture := captureFixture()
	before := time.Date(2026, 8, 31, 23, 59, 0, 0, time.UTC)
	after := time.Date(2026, 9, 1, 0, 0, 1, 0, time.UTC)

	if capture.AppliesTo("", before) {
		t.Fatalf("AppliesTo() before enabled_at = true, want false")
	}
	if !capture.AppliesTo("", after) {
		t.Fatalf("AppliesTo() after enabled_at = false, want true")
	}
}

func TestAppliesToRespectsTheDepartmentScope(t *testing.T) {
	capture := captureFixture()
	capture.DepartmentIDs = []string{"dept1"}
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)

	if !capture.AppliesTo("dept1", now) {
		t.Fatalf("AppliesTo(dept1) = false, want true")
	}
	if capture.AppliesTo("dept2", now) {
		t.Fatalf("AppliesTo(dept2) = true, want false")
	}
	if capture.AppliesTo("", now) {
		t.Fatalf("AppliesTo(no department) = true, want false when the policy is department-scoped")
	}
}

func TestValidateRejectsAReservedCode(t *testing.T) {
	capture := captureFixture()
	capture.Outcomes = append(capture.Outcomes, Outcome{Code: "_system_auto_close", Label: "Falso"})
	capture.Normalize()

	if err := capture.Validate(); !errors.Is(err, ErrOutcomeReserved) {
		t.Fatalf("Validate() = %v, want %v", err, ErrOutcomeReserved)
	}
}

func TestValidateRejectsDuplicatesAndEmptyLabels(t *testing.T) {
	duplicate := captureFixture()
	duplicate.Outcomes = append(duplicate.Outcomes, Outcome{Code: "sale", Label: "Outra venda"})
	duplicate.Normalize()
	if err := duplicate.Validate(); !errors.Is(err, ErrOutcomeDuplicate) {
		t.Fatalf("Validate() on a duplicate code = %v, want %v", err, ErrOutcomeDuplicate)
	}

	unlabelled := captureFixture()
	unlabelled.Outcomes = append(unlabelled.Outcomes, Outcome{Code: "orphan"})
	unlabelled.Normalize()
	if err := unlabelled.Validate(); !errors.Is(err, ErrOutcomeLabelRequired) {
		t.Fatalf("Validate() on a missing label = %v, want %v", err, ErrOutcomeLabelRequired)
	}
}

func TestValidateEnabledCatalogueMustBeUsable(t *testing.T) {
	empty := &OutcomeCapture{Enabled: true, DurableThreshold: 30}
	if err := empty.Validate(); !errors.Is(err, ErrOutcomeCatalogueEmpty) {
		t.Fatalf("Validate() on an empty enabled catalogue = %v, want %v", err, ErrOutcomeCatalogueEmpty)
	}

	noDurable := &OutcomeCapture{
		Enabled:          true,
		DurableThreshold: 30,
		Outcomes:         []Outcome{{Code: "no_answer", Label: "Sem resposta"}},
	}
	if err := noDurable.Validate(); !errors.Is(err, ErrOutcomeNoDurable) {
		t.Fatalf("Validate() with no durable outcome = %v, want %v", err, ErrOutcomeNoDurable)
	}
}

func TestValidateThresholdRange(t *testing.T) {
	capture := captureFixture()
	capture.DurableThreshold = 140
	if err := capture.Validate(); !errors.Is(err, ErrOutcomeThreshold) {
		t.Fatalf("Validate() on an out-of-range threshold = %v, want %v", err, ErrOutcomeThreshold)
	}
}

func TestNormalizeLowercasesAndRenumbers(t *testing.T) {
	capture := &OutcomeCapture{
		Outcomes: []Outcome{
			{Code: "  SALE  ", Label: "  Venda  ", Position: 9},
			{Code: "no_answer", Label: "Sem resposta", Position: 2},
		},
	}
	capture.Normalize()

	if capture.Outcomes[0].Code != "no_answer" || capture.Outcomes[0].Position != 1 {
		t.Fatalf("Normalize() first outcome = %+v, want no_answer at position 1", capture.Outcomes[0])
	}
	if capture.Outcomes[1].Code != "sale" || capture.Outcomes[1].Label != "Venda" {
		t.Fatalf("Normalize() second outcome = %+v, want a trimmed lowercase sale", capture.Outcomes[1])
	}
	if capture.DurableThreshold != DefaultDurableThreshold {
		t.Fatalf("Normalize() DurableThreshold = %v, want %v", capture.DurableThreshold, DefaultDurableThreshold)
	}
}

func TestDurableCodes(t *testing.T) {
	got := captureFixture().DurableCodes()
	if len(got) != 1 || got[0] != "sale" {
		t.Fatalf("DurableCodes() = %v, want [sale]", got)
	}
}
