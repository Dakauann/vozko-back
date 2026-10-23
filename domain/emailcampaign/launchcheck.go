package emailcampaign

import (
	"time"

	"vozko/domain/campaignaudience"
	"vozko/domain/emaildesign"
)

type SenderState string

const (
	SenderReady         SenderState = "ready"
	SenderMissing       SenderState = "missing"
	SenderNotVerified   SenderState = "not_verified"
	SenderAccountPaused SenderState = "account_paused"
)

type IssueCode string

const (
	IssueSenderMissing       IssueCode = "sender_missing"
	IssueSenderNotVerified   IssueCode = "sender_not_verified"
	IssueAccountPaused       IssueCode = "account_paused"
	IssueDocumentInvalid     IssueCode = "document_invalid"
	IssueSubjectInvalid      IssueCode = "subject_invalid"
	IssueAudienceEmpty       IssueCode = "audience_empty"
	IssueBalanceInsufficient IssueCode = "balance_insufficient"
	IssueScheduleInPast      IssueCode = "schedule_in_past"
	IssueDomainsChecking     IssueCode = "domains_checking"
)

func AllIssueCodes() []IssueCode {
	return []IssueCode{IssueSenderMissing, IssueSenderNotVerified, IssueAccountPaused, IssueDocumentInvalid, IssueSubjectInvalid,
		IssueAudienceEmpty, IssueBalanceInsufficient, IssueScheduleInPast, IssueDomainsChecking}
}

type Issue struct {
	Code          IssueCode           `json:"code"`
	MissingMicros int64               `json:"missing_micros,omitempty"`
	Addresses     int                 `json:"addresses,omitempty"`
	Issues        []emaildesign.Issue `json:"issues,omitempty"`
}

type AudienceSummary struct {
	Matched     int                              `json:"matched"`
	WithEmail   int                              `json:"with_email"`
	Sendable    int                              `json:"sendable"`
	NeverMailed int                              `json:"never_mailed"`
	Skipped     map[SkipReason]int               `json:"skipped"`
	Variables   []campaignaudience.VariableCount `json:"-"`
	List        *campaignaudience.ListCount      `json:"-"`
}

func SummaryOf(c campaignaudience.Count) AudienceSummary {
	s := AudienceSummary{Matched: c.Matched, WithEmail: c.WithContact, Skipped: map[SkipReason]int{}, Variables: c.Variables, List: c.List}
	if c.Sendable != nil {
		s.Sendable = *c.Sendable
	}
	for reason, n := range c.Out {
		s.Skipped[SkipReason(reason)] = n
	}
	return s
}

type LaunchInput struct {
	Now           time.Time
	Sender        SenderState
	Document      emaildesign.Document
	Subject       string
	ABTest        *ABTest
	Audience      AudienceSummary
	MissingMicros int64
	ScheduledAt   *time.Time
}

type LaunchReport struct {
	Blocking []Issue            `json:"blocking"`
	Hints    []emaildesign.Hint `json:"hints"`
}

func (r LaunchReport) Ready() bool {
	return len(r.Blocking) == 0
}

func LaunchCheck(in LaunchInput) LaunchReport {
	report := LaunchReport{Blocking: []Issue{}, Hints: emaildesign.Lint(in.Document, in.Subject)}
	if report.Hints == nil {
		report.Hints = []emaildesign.Hint{}
	}
	add := func(issue Issue) { report.Blocking = append(report.Blocking, issue) }
	switch in.Sender {
	case SenderReady:
	case SenderNotVerified:
		add(Issue{Code: IssueSenderNotVerified})
	case SenderAccountPaused:
		add(Issue{Code: IssueAccountPaused})
	default:
		add(Issue{Code: IssueSenderMissing})
	}
	if issues := emaildesign.Validate(in.Document); len(issues) > 0 {
		add(Issue{Code: IssueDocumentInvalid, Issues: issues})
	}
	if issues := subjectIssues(in.Subject, in.ABTest); len(issues) > 0 {
		add(Issue{Code: IssueSubjectInvalid, Issues: issues})
	}
	if in.Audience.Sendable == 0 {
		add(Issue{Code: IssueAudienceEmpty})
	}
	if n := in.Audience.Skipped[SkipDomainChecking]; n > 0 {
		add(Issue{Code: IssueDomainsChecking, Addresses: n})
	}
	if in.MissingMicros > 0 {
		add(Issue{Code: IssueBalanceInsufficient, MissingMicros: in.MissingMicros})
	}
	if in.ScheduledAt != nil && !in.ScheduledAt.After(in.Now) {
		add(Issue{Code: IssueScheduleInPast})
	}
	return report
}

func subjectIssues(subject string, ab *ABTest) []emaildesign.Issue {
	issues := emaildesign.ValidateSubject(subject)
	if ab != nil {
		issues = append(issues, emaildesign.ValidateSubject(ab.VariantBSubject)...)
	}
	return issues
}
