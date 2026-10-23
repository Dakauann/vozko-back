package leadimport

import (
	"vozko/domain/emailhygiene"
	"vozko/domain/lead"
)

var emailReasons = map[emailhygiene.Verdict]lead.RejectReason{
	emailhygiene.VerdictTypo:          lead.ReasonEmailTypo,
	emailhygiene.VerdictDisposable:    lead.ReasonEmailDisposable,
	emailhygiene.VerdictNoMailDomain:  lead.ReasonEmailNoMailDomain,
	emailhygiene.VerdictBadAddress:    lead.ReasonEmailBadAddress,
	emailhygiene.VerdictSuppressed:    lead.ReasonEmailSuppressed,
	emailhygiene.VerdictDomainUnknown: lead.ReasonEmailDomainUnknown,
	emailhygiene.VerdictDomainPending: lead.ReasonEmailDomainUnknown,
	emailhygiene.VerdictRole:          lead.ReasonEmailRole,
	emailhygiene.VerdictInactive:      lead.ReasonEmailInactive,
}

func ApplyEmailAssessment(r *lead.ImportRecord, a emailhygiene.Assessment, g Grants) (lead.ImportIssue, bool) {
	r.EmailVerdict = string(a.Verdict)
	reason, ok := emailReasons[a.Verdict]
	if !ok {
		return lead.ImportIssue{}, false
	}
	issue := lead.ImportIssue{Line: r.Line, Reason: reason, Field: lead.ImportFieldEmail}
	switch {
	case a.Verdict == emailhygiene.VerdictTypo:
		issue.Hint = a.Suggested.String()
		if g.FixEmailTypos && a.Suggested != "" {
			r.Email = a.Suggested.String()
		}
	case a.Verdict.Undeliverable() && g.DropUndeliverableEmails:
		r.Email = ""
	}
	return issue, true
}

type EmailFixable struct {
	Typos         int
	Undeliverable int
}

func (c Counts) EmailFixable() EmailFixable {
	var out EmailFixable
	for verdict, n := range c.Emails {
		v := emailhygiene.Verdict(verdict)
		switch {
		case v == emailhygiene.VerdictTypo:
			out.Typos += n
		case v.Undeliverable():
			out.Undeliverable += n
		}
	}
	return out
}
