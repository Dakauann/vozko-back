package emailhygiene

import (
	"strings"
	"time"
)

type DomainState string

const (
	DomainPending DomainState = "pending"
	DomainAccepts DomainState = "accepts"
	DomainNoMail  DomainState = "no_mail"
	DomainUnknown DomainState = "unknown"
)

func (s DomainState) Valid() bool {
	switch s {
	case DomainPending, DomainAccepts, DomainNoMail, DomainUnknown:
		return true
	}
	return false
}

type DomainDetail string

const (
	DetailNone       DomainDetail = ""
	DetailMX         DomainDetail = "mx"
	DetailImplicitMX DomainDetail = "implicit_mx"
	DetailNullMX     DomainDetail = "null_mx"
	DetailNoRecords  DomainDetail = "no_records"
	DetailTimeout    DomainDetail = "timeout"
	DetailError      DomainDetail = "error"
)

type LookupOutcome int

const (
	LookupNotTried LookupOutcome = iota
	LookupFound
	LookupAbsent
	LookupTimedOut
	LookupFailed
)

type DNSAnswer struct {
	MX      LookupOutcome
	MXHosts []string
	Address LookupOutcome
}

type DomainVerdict struct {
	State  DomainState
	Detail DomainDetail
}

const (
	CanaryDomain      = "gmail.com"
	AcceptsTTL        = 7 * 24 * time.Hour
	NoMailTTL         = 3 * 24 * time.Hour
	LookupTimeout     = 3 * time.Second
	LookupConcurrency = 16
	CheckBatchSize    = 200
	CheckLease        = 2 * time.Minute
)

var unknownRetries = []time.Duration{30 * time.Minute, 2 * time.Hour, 6 * time.Hour, 24 * time.Hour}

func Classify(a DNSAnswer) DomainVerdict {
	switch a.MX {
	case LookupFound:
		return classifyHosts(a.MXHosts)
	case LookupAbsent:
		return classifyAddress(a.Address)
	case LookupTimedOut:
		return DomainVerdict{State: DomainUnknown, Detail: DetailTimeout}
	}
	return DomainVerdict{State: DomainUnknown, Detail: DetailError}
}

func classifyHosts(hosts []string) DomainVerdict {
	for _, host := range hosts {
		if strings.Trim(strings.TrimSpace(host), ".") != "" {
			return DomainVerdict{State: DomainAccepts, Detail: DetailMX}
		}
	}
	if len(hosts) == 0 {
		return DomainVerdict{State: DomainUnknown, Detail: DetailError}
	}
	return DomainVerdict{State: DomainNoMail, Detail: DetailNullMX}
}

func classifyAddress(outcome LookupOutcome) DomainVerdict {
	switch outcome {
	case LookupFound:
		return DomainVerdict{State: DomainAccepts, Detail: DetailImplicitMX}
	case LookupAbsent:
		return DomainVerdict{State: DomainNoMail, Detail: DetailNoRecords}
	case LookupTimedOut:
		return DomainVerdict{State: DomainUnknown, Detail: DetailTimeout}
	}
	return DomainVerdict{State: DomainUnknown, Detail: DetailError}
}

func CanaryHealthy(v DomainVerdict) bool {
	return v.State == DomainAccepts
}

type Settlement struct {
	ExpiresAt time.Time
	DueAt     *time.Time
}

func Settle(v DomainVerdict, attempts int, now time.Time) Settlement {
	switch v.State {
	case DomainAccepts:
		return Settlement{ExpiresAt: now.Add(AcceptsTTL)}
	case DomainNoMail:
		return Settlement{ExpiresAt: now.Add(NoMailTTL)}
	}
	step := min(max(attempts, 1), len(unknownRetries)) - 1
	due := now.Add(unknownRetries[step])
	return Settlement{ExpiresAt: now, DueAt: &due}
}

type DomainCheck struct {
	Domain    string
	State     DomainState
	Detail    DomainDetail
	CheckedAt *time.Time
	ExpiresAt time.Time
	Attempts  int
}

func (c DomainCheck) Stale(now time.Time) bool {
	return c.State == DomainPending || !c.ExpiresAt.After(now)
}
