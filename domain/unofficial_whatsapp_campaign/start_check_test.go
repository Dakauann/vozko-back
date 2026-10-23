package unofficial_whatsapp_campaign

import (
	"errors"
	"fmt"
	"testing"

	"vozko/domain/campaign/launchcheck"
	uw "vozko/domain/unofficial_whatsapp"
)

func TestStartRefusalCodeNamesTheNumberProblem(t *testing.T) {
	cases := map[error]launchcheck.Code{
		NewInstanceUnusableError("5511", "banned"):    launchcheck.CodeNumberUnusable,
		fmt.Errorf("probe: %w", uw.ErrRestrictedByWA): launchcheck.CodeNumberRestricted,
		uw.ErrInstanceNotConnected:                    launchcheck.CodeNumberCannotSend,
		errors.New("gateway down"):                    launchcheck.CodeNumberCannotSend,
	}
	for err, code := range cases {
		if got := StartRefusalCode(err); got != code {
			t.Fatalf("%v: got %s, want %s", err, got, code)
		}
	}
}

func TestStartReportBlocksOnEveryProblem(t *testing.T) {
	if report := StartReport(10, nil, 0); report.Blocks() || len(report.Findings) != 0 {
		t.Fatalf("a healthy start %+v", report)
	}
	report := StartReport(0, uw.ErrRestrictedByWA, 2)
	codes := map[launchcheck.Code]bool{}
	for _, f := range report.Findings {
		if f.Level != launchcheck.Block {
			t.Fatalf("finding %+v is not a block", f)
		}
		codes[f.Code] = true
	}
	if !codes[launchcheck.CodeAudienceEmpty] || !codes[launchcheck.CodeNumberRestricted] || !codes[launchcheck.CodeGroupsUnsendable] {
		t.Fatalf("codes %v", codes)
	}
	if report.Fingerprint() != "" {
		t.Fatal("blocks are never confirmed")
	}
}
