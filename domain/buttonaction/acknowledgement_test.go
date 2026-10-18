package buttonaction

import (
	"errors"
	"testing"
	"time"
)

func TestExitRiskNeedsAnAcknowledgementOnlyWhenTheVerdictIsNone(t *testing.T) {
	given := &Acknowledgement{By: "user-1", At: time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)}
	cases := []struct {
		name  string
		state VerdictState
		ack   *Acknowledgement
		want  error
	}{
		{"ok needs nothing", VerdictOK, nil, nil},
		{"none without acknowledgement", VerdictNone, nil, ErrExitRiskNotAcknowledged},
		{"none with an empty acknowledgement", VerdictNone, &Acknowledgement{}, ErrExitRiskNotAcknowledged},
		{"none without a time", VerdictNone, &Acknowledgement{By: "user-1"}, ErrExitRiskNotAcknowledged},
		{"none acknowledged", VerdictNone, given, nil},
	}
	for _, tc := range cases {
		err := RequireExitRiskAcknowledged(Verdict{State: tc.state}, tc.ack)
		if !errors.Is(err, tc.want) && !(tc.want == nil && err == nil) {
			t.Errorf("%s: got %v, want %v", tc.name, err, tc.want)
		}
	}
	if ErrorCode(ErrExitRiskNotAcknowledged) != "exit_risk_not_acknowledged" {
		t.Fatalf("code = %q", ErrorCode(ErrExitRiskNotAcknowledged))
	}
}
