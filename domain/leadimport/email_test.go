package leadimport

import (
	"testing"

	"vozko/domain/emailhygiene"
	"vozko/domain/lead"
)

func TestEmailVerdictsBecomeImportIssuesAndChoices(t *testing.T) {
	cases := []struct {
		name       string
		assessment emailhygiene.Assessment
		grants     Grants
		wantReason lead.RejectReason
		wantEmail  string
		wantHint   string
	}{
		{name: "a valid address raises nothing", assessment: emailhygiene.Assessment{Verdict: emailhygiene.VerdictValid}, wantEmail: "ana@gmial.com"},
		{name: "a typo is reported with the suggested address", assessment: emailhygiene.Assessment{Verdict: emailhygiene.VerdictTypo, Suggested: "ana@gmail.com"},
			wantReason: lead.ReasonEmailTypo, wantEmail: "ana@gmial.com", wantHint: "ana@gmail.com"},
		{name: "a typo is fixed when the start chose it", assessment: emailhygiene.Assessment{Verdict: emailhygiene.VerdictTypo, Suggested: "ana@gmail.com"},
			grants: Grants{FixEmailTypos: true}, wantReason: lead.ReasonEmailTypo, wantEmail: "ana@gmail.com", wantHint: "ana@gmail.com"},
		{name: "a disposable address is kept unless dropping was chosen", assessment: emailhygiene.Assessment{Verdict: emailhygiene.VerdictDisposable},
			wantReason: lead.ReasonEmailDisposable, wantEmail: "ana@gmial.com"},
		{name: "a domain without mail is dropped when chosen", assessment: emailhygiene.Assessment{Verdict: emailhygiene.VerdictNoMailDomain},
			grants: Grants{DropUndeliverableEmails: true}, wantReason: lead.ReasonEmailNoMailDomain, wantEmail: ""},
		{name: "a shared hard bounce is dropped when chosen", assessment: emailhygiene.Assessment{Verdict: emailhygiene.VerdictBadAddress},
			grants: Grants{DropUndeliverableEmails: true}, wantReason: lead.ReasonEmailBadAddress, wantEmail: ""},
		{name: "a suppressed address is never dropped, the lead keeps it", assessment: emailhygiene.Assessment{Verdict: emailhygiene.VerdictSuppressed},
			grants: Grants{DropUndeliverableEmails: true}, wantReason: lead.ReasonEmailSuppressed, wantEmail: "ana@gmial.com"},
		{name: "an unknown domain is reported", assessment: emailhygiene.Assessment{Verdict: emailhygiene.VerdictDomainUnknown}, wantReason: lead.ReasonEmailDomainUnknown, wantEmail: "ana@gmial.com"},
		{name: "a domain still pending is unknown for the import", assessment: emailhygiene.Assessment{Verdict: emailhygiene.VerdictDomainPending}, wantReason: lead.ReasonEmailDomainUnknown, wantEmail: "ana@gmial.com"},
		{name: "a role address is a warning", assessment: emailhygiene.Assessment{Verdict: emailhygiene.VerdictRole}, wantReason: lead.ReasonEmailRole, wantEmail: "ana@gmial.com"},
		{name: "a sunset contact is reported", assessment: emailhygiene.Assessment{Verdict: emailhygiene.VerdictInactive}, wantReason: lead.ReasonEmailInactive, wantEmail: "ana@gmial.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := lead.ImportRecord{Line: 7, Email: "ana@gmial.com"}
			issue, ok := ApplyEmailAssessment(&r, tc.assessment, tc.grants)
			if tc.wantReason == "" {
				if ok {
					t.Fatalf("issue = %+v", issue)
				}
			} else if !ok || issue.Reason != tc.wantReason || issue.Line != 7 || issue.Field != lead.ImportFieldEmail || issue.Rejected || issue.Hint != tc.wantHint {
				t.Fatalf("issue = %+v %v", issue, ok)
			}
			if r.Email != tc.wantEmail || r.EmailVerdict != string(tc.assessment.Verdict) {
				t.Fatalf("record = %+v", r)
			}
		})
	}
}

func TestCountsTallyEmailVerdicts(t *testing.T) {
	c := NewCounts()
	c.RecordRow(lead.ImportRecord{EmailVerdict: string(emailhygiene.VerdictValid)}, lead.ImportDecision{Verdict: lead.ImportCreated}, nil)
	c.RecordRow(lead.ImportRecord{EmailVerdict: string(emailhygiene.VerdictValid)}, lead.ImportDecision{Verdict: lead.ImportCreated}, nil)
	c.RecordRow(lead.ImportRecord{EmailVerdict: string(emailhygiene.VerdictTypo)}, lead.ImportDecision{Verdict: lead.ImportCreated}, nil)
	c.RecordRow(lead.ImportRecord{}, lead.ImportDecision{Verdict: lead.ImportCreated}, nil)
	if c.Emails["valid"] != 2 || c.Emails["typo"] != 1 || len(c.Emails) != 2 {
		t.Fatalf("emails = %v, rows without an email are not counted", c.Emails)
	}
	clone := c.Clone()
	clone.Emails["valid"]++
	if c.Emails["valid"] != 2 {
		t.Fatal("a clone never shares the tally")
	}
}

func TestEmailFixableCountsWhatEachStartChoiceWouldChange(t *testing.T) {
	c := Counts{Emails: map[string]int{"valid": 9, "typo": 3, "disposable": 2, "no_mail_domain": 1, "bad_address": 4, "suppressed": 5, "role": 6}}
	if got := c.EmailFixable(); got != (EmailFixable{Typos: 3, Undeliverable: 7}) {
		t.Fatalf("fixable = %+v, a suppressed address is kept and a role address is only a warning", got)
	}
	if got := (Counts{}).EmailFixable(); got != (EmailFixable{}) {
		t.Fatalf("no emails = %+v", got)
	}
}
