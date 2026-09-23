package attendance

import (
	"testing"
	"time"
)

func enabledAt(t *testing.T, value string) *time.Time {
	t.Helper()
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		t.Fatalf("Parse(%q) error = %v", value, err)
	}
	return &parsed
}

func TestBuildQualityWithoutAnEnabledAtIsUnavailable(t *testing.T) {
	got := BuildQuality([]QualityTally{
		{ActorID: "u1", ActorKind: ActorKindHuman, Closes: 10, Captured: 10, Durable: 6},
	}, 30, nil, 0)

	if got.Available {
		t.Fatalf("BuildQuality() without enabled_at Available = true, want false")
	}
	if got.Reason != ReasonCaptureNotDated {
		t.Fatalf("BuildQuality() Reason = %q, want %q", got.Reason, ReasonCaptureNotDated)
	}
	if len(got.Rows) != 0 {
		t.Fatalf("BuildQuality() produced rows without a dated policy")
	}
}

func TestBuildQualitySplitsHumansFromAdjacents(t *testing.T) {
	since := enabledAt(t, "2026-09-01")
	got := BuildQuality([]QualityTally{
		{ActorID: "u1", ActorKind: ActorKindHuman, DisplayName: "Bella", Closes: 364, Captured: 364, Durable: 220},
		{ActorID: "u2", ActorKind: ActorKindHuman, DisplayName: "Caio", Closes: 41, Captured: 41, Durable: 6},
		{ActorID: "sys", ActorKind: ActorKindSystem, DisplayName: "Sistema", Closes: 1280, Captured: 0, Durable: 0},
		{ActorID: "ai", ActorKind: ActorKindAI, DisplayName: "IA", Closes: 90, Captured: 0, Durable: 0},
	}, 30, since, 12)

	if !got.Available {
		t.Fatalf("BuildQuality() Available = false, want true")
	}
	if len(got.Rows) != 2 {
		t.Fatalf("BuildQuality() human rows = %d, want 2", len(got.Rows))
	}
	if len(got.Adjacent) != 2 {
		t.Fatalf("BuildQuality() adjacent rows = %d, want 2", len(got.Adjacent))
	}
	for _, row := range got.Adjacent {
		if row.Verdict != "" {
			t.Fatalf("BuildQuality() adjacent row %q carries verdict %q, want none", row.DisplayName, row.Verdict)
		}
	}
	if got.Team.Captured != 405 || got.Team.Durable != 226 {
		t.Fatalf("BuildQuality() team = %+v, want 405 captured and 226 durable over humans only", got.Team)
	}
	if got.NotCaptured != 12 {
		t.Fatalf("BuildQuality() NotCaptured = %d, want 12", got.NotCaptured)
	}
}

func TestBuildQualityVerdictAgainstTheThreshold(t *testing.T) {
	since := enabledAt(t, "2026-09-01")
	got := BuildQuality([]QualityTally{
		{ActorID: "u1", ActorKind: ActorKindHuman, DisplayName: "Bella", Closes: 364, Captured: 364, Durable: 220},
		{ActorID: "u2", ActorKind: ActorKindHuman, DisplayName: "Caio", Closes: 41, Captured: 41, Durable: 6},
	}, 30, since, 0)

	byName := map[string]QualityRow{}
	for _, row := range got.Rows {
		byName[row.DisplayName] = row
	}
	if byName["Bella"].Verdict != VerdictOnTrack {
		t.Fatalf("BuildQuality() Bella verdict = %q, want %q", byName["Bella"].Verdict, VerdictOnTrack)
	}
	if byName["Caio"].Verdict != VerdictOffTrack {
		t.Fatalf("BuildQuality() Caio verdict = %q, want %q", byName["Caio"].Verdict, VerdictOffTrack)
	}
}

func TestBuildQualityZeroCapturedIsInsufficientData(t *testing.T) {
	since := enabledAt(t, "2026-09-01")
	got := BuildQuality([]QualityTally{
		{ActorID: "u1", ActorKind: ActorKindHuman, DisplayName: "Nova", Closes: 5, Captured: 0, Durable: 0},
	}, 30, since, 40)

	if got.Available {
		t.Fatalf("BuildQuality() with nothing captured Available = true, want false")
	}
	if got.Reason != ReasonNothingCaptured {
		t.Fatalf("BuildQuality() Reason = %q, want %q", got.Reason, ReasonNothingCaptured)
	}
	if got.Rows[0].DurablePct != nil {
		t.Fatalf("BuildQuality() DurablePct = %v, want nil with a zero denominator", *got.Rows[0].DurablePct)
	}
	if got.Rows[0].Verdict != VerdictInsufficientData {
		t.Fatalf("BuildQuality() verdict = %q, want %q", got.Rows[0].Verdict, VerdictInsufficientData)
	}
}

func TestBuildQualityClampsDurableToCaptured(t *testing.T) {
	since := enabledAt(t, "2026-09-01")
	got := BuildQuality([]QualityTally{
		{ActorID: "u1", ActorKind: ActorKindHuman, Closes: 10, Captured: 4, Durable: 9},
	}, 30, since, 0)

	if got.Rows[0].Durable != 4 {
		t.Fatalf("BuildQuality() Durable = %d, want it clamped to the captured count", got.Rows[0].Durable)
	}
	if got.Rows[0].DurablePct == nil || *got.Rows[0].DurablePct != 100 {
		t.Fatalf("BuildQuality() DurablePct = %v, want 100", got.Rows[0].DurablePct)
	}
}

func TestUnavailableQualityCarriesItsReason(t *testing.T) {
	got := UnavailableQuality(ReasonCaptureDisabled)
	if got.Available || got.Reason != ReasonCaptureDisabled {
		t.Fatalf("UnavailableQuality() = %+v, want unavailable with %q", got, ReasonCaptureDisabled)
	}
	if got.Rows == nil || got.Adjacent == nil {
		t.Fatalf("UnavailableQuality() left a nil slice on the wire")
	}
}

func TestQualityPolicyMeasurable(t *testing.T) {
	if (QualityPolicy{Enabled: true}).Measurable() {
		t.Fatalf("QualityPolicy without enabled_at Measurable() = true, want false")
	}
	if (QualityPolicy{Enabled: false, EnabledAt: enabledAt(t, "2026-09-01")}).Measurable() {
		t.Fatalf("QualityPolicy disabled Measurable() = true, want false")
	}
	if !(QualityPolicy{Enabled: true, EnabledAt: enabledAt(t, "2026-09-01")}).Measurable() {
		t.Fatalf("QualityPolicy enabled and dated Measurable() = false, want true")
	}
}
