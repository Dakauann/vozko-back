package geocoding

import (
	"context"
	"net/http"
	"slices"
	"time"

	"vozko/domain/geo"
)

type Refusal string

const (
	RefusalNone    Refusal = ""
	RefusalQuery   Refusal = "query"
	RefusalAccount Refusal = "account"
)

const (
	AccountPause    = time.Hour
	MaxAccountPause = 24 * time.Hour

	QueryRefusalsBeforePause = 2
)

func ReasonOfStatus(status int) geo.UnavailableReason {
	switch {
	case status == http.StatusTooManyRequests:
		return geo.ReasonRateLimited
	case status == http.StatusRequestTimeout || status >= http.StatusInternalServerError:
		return geo.ReasonProviderDown
	case status == http.StatusBadRequest || status == http.StatusGone || status == http.StatusRequestURITooLong:
		return geo.ReasonQueryRefused
	case status == http.StatusUnauthorized:
		return geo.ReasonKeyRejected
	case status == http.StatusPaymentRequired:
		return geo.ReasonAccountQuotaSpent
	case status == http.StatusForbidden:
		return geo.ReasonKeyDisabled
	}
	return geo.ReasonAccountRefused
}

func RefusalOf(o geo.Outcome) Refusal {
	if o.Kind != geo.OutcomeUnavailable {
		return RefusalNone
	}
	switch {
	case o.Reason == geo.ReasonQueryRefused:
		return RefusalQuery
	case slices.Contains(AccountRefusalReasons(), o.Reason):
		return RefusalAccount
	}
	return RefusalNone
}

func AccountRefusalReasons() []geo.UnavailableReason {
	return []geo.UnavailableReason{geo.ReasonKeyRejected, geo.ReasonAccountQuotaSpent, geo.ReasonKeyDisabled, geo.ReasonAccountRefused, geo.ReasonQueriesRefused}
}

type ProviderPause struct {
	Provider Provider
	Reason   geo.UnavailableReason
	Since    time.Time
	Until    time.Time
}

func PauseAfter(provider Provider, answer geo.Outcome, now time.Time) (ProviderPause, bool) {
	if RefusalOf(answer) != RefusalAccount {
		return ProviderPause{}, false
	}
	length := min(max(AccountPause, answer.RetryAfter), MaxAccountPause)
	return ProviderPause{Provider: provider, Reason: answer.Reason, Since: now, Until: now.Add(length)}, true
}

func PauseAfterQueryRefusals(provider Provider, refusedTexts int, now time.Time) (ProviderPause, bool) {
	if refusedTexts < QueryRefusalsBeforePause {
		return ProviderPause{}, false
	}
	return ProviderPause{Provider: provider, Reason: geo.ReasonQueriesRefused, Since: now, Until: now.Add(AccountPause)}, true
}

func LongerPause(a, b ProviderPause) ProviderPause {
	if b.Until.After(a.Until) {
		return b
	}
	return a
}

func (p ProviderPause) Valid() bool {
	return p.Provider.Known() && slices.Contains(AccountRefusalReasons(), p.Reason) && !p.Since.IsZero() && p.Until.After(p.Since)
}

func (p ProviderPause) ActiveAt(now time.Time) bool { return now.Before(p.Until) }

func (p ProviderPause) Remaining(now time.Time) time.Duration { return max(p.Until.Sub(now), 0) }

func (p ProviderPause) Outcome(now time.Time) geo.Outcome {
	return geo.Unavailable(geo.ReasonProviderPaused, p.Remaining(now))
}

type PauseReader interface {
	ProviderPause(ctx context.Context, provider Provider) (ProviderPause, bool, error)
}

type PauseStore interface {
	PauseReader
	OpenProviderPause(ctx context.Context, pause ProviderPause) error
}
