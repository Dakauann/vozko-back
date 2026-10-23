package emailhygiene

import (
	"errors"
	"time"

	es "vozko/domain/emailsending"
)

const (
	CodeDomainChecking    = "email_domain_checking"
	CodeDomainUnknown     = "email_domain_unknown"
	CodeDisposableAddress = "email_disposable_address"
	CodeNoMailDomain      = "email_no_mail_domain"
)

var ErrRecipientRefused = errors.New("emailhygiene: list hygiene refused this recipient")

var oneToOneCodes = map[Verdict]string{
	VerdictNoMailDomain:  CodeNoMailDomain,
	VerdictDisposable:    CodeDisposableAddress,
	VerdictDomainUnknown: CodeDomainUnknown,
	VerdictDomainPending: CodeDomainChecking,
}

type RecipientRefusal struct {
	Code string
}

func (r *RecipientRefusal) Error() string {
	return "emailhygiene: recipient refused (" + r.Code + ")"
}

func (r *RecipientRefusal) Is(target error) bool {
	return target == ErrRecipientRefused
}

func FollowsCampaignRule(kind es.RunKind) bool {
	return kind != es.RunAPI && kind != es.RunCampaign
}

func OneToOneRefusal(kind es.RunKind, f Facts, s Standing, now time.Time) *RecipientRefusal {
	if !FollowsCampaignRule(kind) {
		return nil
	}
	code, refused := oneToOneCodes[Assess(f, s, now)]
	if !refused {
		return nil
	}
	return &RecipientRefusal{Code: code}
}

func (s Standing) NeedsDomainCheck() bool {
	return s.DomainStale || s.Domain == "" || s.Domain == DomainPending
}

func RecipientRefusalCode(err error) (string, bool) {
	var refusal *RecipientRefusal
	if !errors.As(err, &refusal) {
		return "", false
	}
	return refusal.Code, true
}

func RetryableRefusal(err error) bool {
	code, ok := RecipientRefusalCode(err)
	return ok && code == CodeDomainChecking
}
