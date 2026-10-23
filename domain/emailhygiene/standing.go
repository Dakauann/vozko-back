package emailhygiene

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"vozko/domain/emailaddress"
	es "vozko/domain/emailsending"
)

type AddressHash [sha256.Size]byte

func HashOf(a emailaddress.Address) AddressHash {
	return AddressHash(sha256.Sum256([]byte(a)))
}

func (h AddressHash) Hex() string {
	return hex.EncodeToString(h[:])
}

type Standing struct {
	Suppression  es.SuppressReason
	SuppressedAt *time.Time
	BadAddress   bool
	Domain       DomainState
	DomainStale  bool
	Activity     Activity
}

func (s Standing) Refusal() es.SuppressReason {
	switch {
	case s.Suppression != "":
		return s.Suppression
	case s.BadAddress:
		return es.SuppressBadAddress
	case s.Domain == DomainNoMail:
		return es.SuppressNoMailDomain
	}
	return ""
}

type Facts struct {
	Disposable bool
	Role       bool
	Suggestion string
}

type Checker struct {
	disposable *DisposableSet
	typos      *TypoIndex
}

func NewChecker(disposable *DisposableSet, typos *TypoIndex) Checker {
	return Checker{disposable: disposable, typos: typos}
}

func (c Checker) Facts(a emailaddress.Address) Facts {
	f := Facts{Disposable: c.disposable.Contains(a.Domain()), Role: IsRole(a)}
	if suggestion, ok := c.typos.Suggest(a.Domain()); ok {
		f.Suggestion = suggestion
	}
	return f
}

type Verdict string

const (
	VerdictValid         Verdict = "valid"
	VerdictSuppressed    Verdict = "suppressed"
	VerdictBadAddress    Verdict = "bad_address"
	VerdictNoMailDomain  Verdict = "no_mail_domain"
	VerdictDisposable    Verdict = "disposable"
	VerdictDomainUnknown Verdict = "domain_unknown"
	VerdictDomainPending Verdict = "domain_pending"
	VerdictInactive      Verdict = "inactive"
	VerdictTypo          Verdict = "typo"
	VerdictRole          Verdict = "role"
)

func AllVerdicts() []Verdict {
	return []Verdict{VerdictValid, VerdictSuppressed, VerdictBadAddress, VerdictNoMailDomain, VerdictDisposable,
		VerdictDomainUnknown, VerdictDomainPending, VerdictInactive, VerdictTypo, VerdictRole}
}

func (v Verdict) BlocksCampaigns() bool {
	switch v {
	case VerdictSuppressed, VerdictBadAddress, VerdictNoMailDomain, VerdictDisposable, VerdictDomainUnknown, VerdictDomainPending, VerdictInactive:
		return true
	}
	return false
}

func (v Verdict) Undeliverable() bool {
	return v == VerdictBadAddress || v == VerdictNoMailDomain || v == VerdictDisposable
}

func Assess(f Facts, s Standing, now time.Time) Verdict {
	switch {
	case s.Suppression != "":
		return VerdictSuppressed
	case s.BadAddress:
		return VerdictBadAddress
	case s.Domain == DomainNoMail:
		return VerdictNoMailDomain
	case f.Disposable:
		return VerdictDisposable
	case s.Domain == DomainUnknown:
		return VerdictDomainUnknown
	case s.Domain == DomainPending || s.Domain == "":
		return VerdictDomainPending
	case s.Activity.Inactive(now):
		return VerdictInactive
	case f.Suggestion != "":
		return VerdictTypo
	case f.Role:
		return VerdictRole
	}
	return VerdictValid
}

type Assessment struct {
	Verdict   Verdict
	Suggested emailaddress.Address
}
