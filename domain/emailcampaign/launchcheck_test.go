package emailcampaign

import (
	"testing"
	"time"

	"vozko/domain/emaildesign"
)

func readyDocument() emaildesign.Document {
	return emaildesign.Document{
		Version: emaildesign.CurrentVersion,
		Blocks: []emaildesign.Block{
			{ID: "h1", Kind: emaildesign.KindHeading, Heading: &emaildesign.Heading{Text: "Olá {{first_name|cliente}}", Level: 1}},
			{ID: "f1", Kind: emaildesign.KindFooter, Footer: &emaildesign.Footer{CompanyName: "Loja", Address: "Rua A, 1"}},
		},
	}
}

func readyInput() LaunchInput {
	return LaunchInput{
		Now:      time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC),
		Sender:   SenderReady,
		Document: readyDocument(),
		Subject:  "Novidades da semana",
		Audience: AudienceSummary{Matched: 100, Sendable: 80},
	}
}

func codes(r LaunchReport) []IssueCode {
	out := make([]IssueCode, 0, len(r.Blocking))
	for _, i := range r.Blocking {
		out = append(out, i.Code)
	}
	return out
}

func TestLaunchCheck(t *testing.T) {
	past := time.Date(2026, 10, 21, 11, 0, 0, 0, time.UTC)
	future := time.Date(2026, 10, 22, 11, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		mutate func(*LaunchInput)
		want   []IssueCode
	}{
		{name: "ready", mutate: func(*LaunchInput) {}, want: nil},
		{name: "no sender", mutate: func(in *LaunchInput) { in.Sender = SenderMissing }, want: []IssueCode{IssueSenderMissing}},
		{name: "unverified sender", mutate: func(in *LaunchInput) { in.Sender = SenderNotVerified }, want: []IssueCode{IssueSenderNotVerified}},
		{name: "paused account", mutate: func(in *LaunchInput) { in.Sender = SenderAccountPaused }, want: []IssueCode{IssueAccountPaused}},
		{name: "empty document", mutate: func(in *LaunchInput) { in.Document = emaildesign.Document{} }, want: []IssueCode{IssueDocumentInvalid}},
		{name: "bad subject", mutate: func(in *LaunchInput) { in.Subject = "" }, want: []IssueCode{IssueSubjectInvalid}},
		{name: "bad b subject", mutate: func(in *LaunchInput) {
			in.ABTest = &ABTest{VariantBSubject: "{{nada}}", TestPercent: 10, WinnerMetric: WinnerOpens, DecideAfterMinutes: 30}
		}, want: []IssueCode{IssueSubjectInvalid}},
		{name: "nobody to send to", mutate: func(in *LaunchInput) { in.Audience = AudienceSummary{Matched: 10} }, want: []IssueCode{IssueAudienceEmpty}},
		{name: "short wallet", mutate: func(in *LaunchInput) { in.MissingMicros = 1234 }, want: []IssueCode{IssueBalanceInsufficient}},
		{name: "past schedule", mutate: func(in *LaunchInput) { in.ScheduledAt = &past }, want: []IssueCode{IssueScheduleInPast}},
		{name: "future schedule", mutate: func(in *LaunchInput) { in.ScheduledAt = &future }, want: nil},
		{name: "everything wrong", mutate: func(in *LaunchInput) {
			in.Sender = SenderMissing
			in.Subject = ""
			in.MissingMicros = 1
		}, want: []IssueCode{IssueSenderMissing, IssueSubjectInvalid, IssueBalanceInsufficient}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := readyInput()
			tc.mutate(&in)
			report := LaunchCheck(in)
			got := codes(report)
			if len(got) != len(tc.want) {
				t.Fatalf("blocking = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("blocking = %v, want %v", got, tc.want)
				}
			}
			if report.Ready() != (len(tc.want) == 0) {
				t.Fatal("Ready() disagrees with the blocking list")
			}
		})
	}
}

func TestLaunchCheckCarriesDetails(t *testing.T) {
	in := readyInput()
	in.MissingMicros = 5000
	in.Document = emaildesign.Document{}
	report := LaunchCheck(in)
	for _, issue := range report.Blocking {
		switch issue.Code {
		case IssueBalanceInsufficient:
			if issue.MissingMicros != 5000 {
				t.Errorf("missing = %d", issue.MissingMicros)
			}
		case IssueDocumentInvalid:
			if len(issue.Issues) == 0 {
				t.Error("document issues are listed")
			}
		}
	}
}

func TestEveryAudienceWithSendableAddressesPassesTheListCheck(t *testing.T) {
	in := readyInput()
	in.Audience = AudienceSummary{Matched: 3, WithEmail: 3, Sendable: 1, Skipped: map[SkipReason]int{SkipUnsubscribed: 1, SkipSuppressed: 1}}
	if r := LaunchCheck(in); !r.Ready() {
		t.Fatalf("blocking = %v", codes(r))
	}
	for _, code := range AllIssueCodes() {
		if code == "list_not_validated" || code == "list_blocked" {
			t.Fatalf("%q is not a launch issue any more", code)
		}
	}
}

func TestLaunchCheckHintsComeFromTheLinter(t *testing.T) {
	in := readyInput()
	report := LaunchCheck(in)
	want := emaildesign.Lint(in.Document, in.Subject)
	if len(report.Hints) != len(want) {
		t.Fatalf("hints = %+v, want %+v", report.Hints, want)
	}
}
