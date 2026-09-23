package workspace_config

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/conversation"
)

func usableCapture() *conversation.OutcomeCapture {
	return &conversation.OutcomeCapture{
		Enabled:          true,
		RequireOnFinish:  true,
		DurableThreshold: 30,
		Outcomes: []conversation.Outcome{
			{Code: "sale", Label: "Venda fechada", IsDurable: true},
			{Code: "no_answer", Label: "Sem resposta"},
		},
	}
}

func TestMergeStampsEnabledAtOnFirstActivation(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

	merged, err := MergeOutcomeCapture(nil, usableCapture(), now)
	if err != nil {
		t.Fatalf("MergeOutcomeCapture() err = %v, want nil", err)
	}
	if merged.EnabledAt == nil || !merged.EnabledAt.Equal(now) {
		t.Fatalf("MergeOutcomeCapture() EnabledAt = %v, want %v", merged.EnabledAt, now)
	}
}

func TestMergeKeepsTheOriginalEnabledAtWhileStillOn(t *testing.T) {
	first := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	later := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

	existing := usableCapture()
	existing.EnabledAt = &first

	incoming := usableCapture()
	incoming.DurableThreshold = 45

	merged, err := MergeOutcomeCapture(existing, incoming, later)
	if err != nil {
		t.Fatalf("MergeOutcomeCapture() err = %v, want nil", err)
	}
	if merged.EnabledAt == nil || !merged.EnabledAt.Equal(first) {
		t.Fatalf("MergeOutcomeCapture() moved EnabledAt to %v; editing the catalogue must not reset the denominator", merged.EnabledAt)
	}
	if merged.DurableThreshold != 45 {
		t.Fatalf("MergeOutcomeCapture() DurableThreshold = %v, want 45", merged.DurableThreshold)
	}
}

func TestMergeClearsEnabledAtWhenSwitchedOff(t *testing.T) {
	first := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	existing := usableCapture()
	existing.EnabledAt = &first

	incoming := usableCapture()
	incoming.Enabled = false

	merged, err := MergeOutcomeCapture(existing, incoming, time.Now())
	if err != nil {
		t.Fatalf("MergeOutcomeCapture() err = %v, want nil", err)
	}
	if merged.EnabledAt != nil {
		t.Fatalf("MergeOutcomeCapture() EnabledAt = %v, want nil once capture is off", merged.EnabledAt)
	}
}

func TestMergeRestampsAfterAGapInCapture(t *testing.T) {
	off := usableCapture()
	off.Enabled = false
	off.EnabledAt = nil

	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	merged, err := MergeOutcomeCapture(off, usableCapture(), now)
	if err != nil {
		t.Fatalf("MergeOutcomeCapture() err = %v, want nil", err)
	}
	if merged.EnabledAt == nil || !merged.EnabledAt.Equal(now) {
		t.Fatalf("MergeOutcomeCapture() EnabledAt = %v, want the new activation %v", merged.EnabledAt, now)
	}
}

func TestMergeRejectsAnUnusablePolicy(t *testing.T) {
	reserved := usableCapture()
	reserved.Outcomes = append(reserved.Outcomes, conversation.Outcome{
		Code:  "_system_auto_close",
		Label: "Falso",
	})
	if _, err := MergeOutcomeCapture(nil, reserved, time.Now()); !errors.Is(err, conversation.ErrOutcomeReserved) {
		t.Fatalf("MergeOutcomeCapture() err = %v, want %v", err, conversation.ErrOutcomeReserved)
	}

	noDurable := usableCapture()
	noDurable.Outcomes = []conversation.Outcome{{Code: "no_answer", Label: "Sem resposta"}}
	if _, err := MergeOutcomeCapture(nil, noDurable, time.Now()); !errors.Is(err, conversation.ErrOutcomeNoDurable) {
		t.Fatalf("MergeOutcomeCapture() err = %v, want %v", err, conversation.ErrOutcomeNoDurable)
	}
}

func TestMergeNormalizesBeforeStoring(t *testing.T) {
	incoming := &conversation.OutcomeCapture{
		Enabled: true,
		Outcomes: []conversation.Outcome{
			{Code: "  SALE  ", Label: "  Venda  ", IsDurable: true, Position: 7},
		},
	}

	merged, err := MergeOutcomeCapture(nil, incoming, time.Now())
	if err != nil {
		t.Fatalf("MergeOutcomeCapture() err = %v, want nil", err)
	}
	if merged.Outcomes[0].Code != "sale" || merged.Outcomes[0].Label != "Venda" {
		t.Fatalf("MergeOutcomeCapture() stored %+v, want a trimmed lowercase code", merged.Outcomes[0])
	}
	if merged.Outcomes[0].Position != 1 {
		t.Fatalf("MergeOutcomeCapture() Position = %d, want 1", merged.Outcomes[0].Position)
	}
	if merged.DurableThreshold != conversation.DefaultDurableThreshold {
		t.Fatalf("MergeOutcomeCapture() DurableThreshold = %v, want the default", merged.DurableThreshold)
	}
}

func TestMergeWithNoIncomingKeepsTheExisting(t *testing.T) {
	existing := usableCapture()
	merged, err := MergeOutcomeCapture(existing, nil, time.Now())
	if err != nil {
		t.Fatalf("MergeOutcomeCapture() err = %v, want nil", err)
	}
	if merged != existing {
		t.Fatalf("MergeOutcomeCapture() replaced the stored policy when nothing was sent")
	}
}
