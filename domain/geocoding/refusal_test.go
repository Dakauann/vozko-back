package geocoding

import (
	"net/http"
	"testing"
	"time"

	"vozko/domain/geo"
)

func TestReasonOfStatusSplitsRefusalsByWhatTheyMean(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   geo.UnavailableReason
		class  Refusal
	}{
		{"a bad request refuses the query text", http.StatusBadRequest, geo.ReasonQueryRefused, RefusalQuery},
		{"a request too long refuses the query text", http.StatusGone, geo.ReasonQueryRefused, RefusalQuery},
		{"a URI too long refuses the query text", http.StatusRequestURITooLong, geo.ReasonQueryRefused, RefusalQuery},
		{"a rejected key refuses the account", http.StatusUnauthorized, geo.ReasonKeyRejected, RefusalAccount},
		{"a spent account quota refuses the account", http.StatusPaymentRequired, geo.ReasonAccountQuotaSpent, RefusalAccount},
		{"a disabled key refuses the account", http.StatusForbidden, geo.ReasonKeyDisabled, RefusalAccount},
		{"an unknown endpoint refuses the account", http.StatusNotFound, geo.ReasonAccountRefused, RefusalAccount},
		{"an unexpected client status refuses the account", http.StatusUpgradeRequired, geo.ReasonAccountRefused, RefusalAccount},
		{"too many requests is transient", http.StatusTooManyRequests, geo.ReasonRateLimited, RefusalNone},
		{"a timeout is transient", http.StatusRequestTimeout, geo.ReasonProviderDown, RefusalNone},
		{"a server error is transient", http.StatusInternalServerError, geo.ReasonProviderDown, RefusalNone},
		{"an unavailable server is transient", http.StatusServiceUnavailable, geo.ReasonProviderDown, RefusalNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ReasonOfStatus(tt.status)
			if got != tt.want {
				t.Fatalf("ReasonOfStatus(%d) = %q, want %q", tt.status, got, tt.want)
			}
			if class := RefusalOf(geo.Unavailable(got, 0)); class != tt.class {
				t.Fatalf("RefusalOf(%q) = %q, want %q", got, class, tt.class)
			}
		})
	}
}

func TestRefusalOfOnlyCountsUnavailableProviderAnswers(t *testing.T) {
	tests := []struct {
		name    string
		outcome geo.Outcome
		want    Refusal
	}{
		{"a located answer", geo.Located(providerFix(geo.PrecisionAddress)), RefusalNone},
		{"no result", geo.NotFound(), RefusalNone},
		{"an ambiguous answer", geo.Ambiguous(), RefusalNone},
		{"a provider that is down", geo.Unavailable(geo.ReasonProviderDown, 0), RefusalNone},
		{"an unreadable answer", geo.Unavailable(geo.ReasonInvalidAnswer, 0), RefusalNone},
		{"a paused provider", geo.Unavailable(geo.ReasonProviderPaused, time.Hour), RefusalNone},
		{"a refused query", geo.Unavailable(geo.ReasonQueryRefused, 0), RefusalQuery},
		{"a rejected key", geo.Unavailable(geo.ReasonKeyRejected, 0), RefusalAccount},
		{"a spent account", geo.Unavailable(geo.ReasonAccountQuotaSpent, 0), RefusalAccount},
		{"a disabled key", geo.Unavailable(geo.ReasonKeyDisabled, 0), RefusalAccount},
		{"any other account refusal", geo.Unavailable(geo.ReasonAccountRefused, 0), RefusalAccount},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RefusalOf(tt.outcome); got != tt.want {
				t.Fatalf("RefusalOf() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPauseAfterOpensABoundedPauseOnlyForAnAccountRefusal(t *testing.T) {
	tests := []struct {
		name    string
		outcome geo.Outcome
		paused  bool
		until   time.Duration
	}{
		{"a rejected key pauses the provider", geo.Unavailable(geo.ReasonKeyRejected, 0), true, AccountPause},
		{"a spent account waits for the provider's reset", geo.Unavailable(geo.ReasonAccountQuotaSpent, 5*time.Hour), true, 5 * time.Hour},
		{"a reset beyond a day is capped", geo.Unavailable(geo.ReasonAccountQuotaSpent, 72*time.Hour), true, MaxAccountPause},
		{"a disabled key pauses the provider", geo.Unavailable(geo.ReasonKeyDisabled, time.Minute), true, AccountPause},
		{"a refused query never pauses the provider", geo.Unavailable(geo.ReasonQueryRefused, 0), false, 0},
		{"a transient failure never pauses the provider", geo.Unavailable(geo.ReasonProviderDown, 0), false, 0},
		{"a rate limit never pauses the provider", geo.Unavailable(geo.ReasonRateLimited, time.Hour), false, 0},
		{"an answer never pauses the provider", geo.NotFound(), false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pause, paused := PauseAfter(ProviderOpenCage, tt.outcome, at)
			if paused != tt.paused {
				t.Fatalf("PauseAfter() paused = %v, want %v", paused, tt.paused)
			}
			if !paused {
				return
			}
			if pause.Provider != ProviderOpenCage || pause.Reason != tt.outcome.Reason || !pause.Since.Equal(at) || !pause.Until.Equal(at.Add(tt.until)) {
				t.Fatalf("PauseAfter() = %+v, want %s from %v until %v", pause, tt.outcome.Reason, at, at.Add(tt.until))
			}
			if !pause.ActiveAt(at) || !pause.ActiveAt(pause.Until.Add(-time.Second)) || pause.ActiveAt(pause.Until) {
				t.Fatalf("pause %+v must hold from its start until, and not at, its end", pause)
			}
			if pause.Remaining(at) != tt.until || pause.Remaining(pause.Until.Add(time.Hour)) != 0 {
				t.Fatalf("Remaining() = %v, want %v and never negative", pause.Remaining(at), tt.until)
			}
		})
	}
}

func TestAPausedProviderAnswersUnavailableUntilThePauseEnds(t *testing.T) {
	pause, _ := PauseAfter(ProviderOpenCage, geo.Unavailable(geo.ReasonKeyRejected, 0), at)
	got := pause.Outcome(at.Add(10 * time.Minute))
	if got.Kind != geo.OutcomeUnavailable || got.Reason != geo.ReasonProviderPaused || got.RetryAfter != AccountPause-10*time.Minute {
		t.Fatalf("Outcome() = %+v, want unavailable for the rest of the pause", got)
	}
	if RefusalOf(got) != RefusalNone {
		t.Fatal("a paused provider answers for itself and never opens another pause")
	}
}

func TestAccountRefusalReasonsAreExactlyTheOnesThatPause(t *testing.T) {
	for _, reason := range AccountRefusalReasons() {
		if RefusalOf(geo.Unavailable(reason, 0)) != RefusalAccount {
			t.Fatalf("%q is listed as an account refusal but does not pause", reason)
		}
	}
	if len(AccountRefusalReasons()) != 5 {
		t.Fatalf("AccountRefusalReasons() = %v, want the four account refusals and a run of refused texts", AccountRefusalReasons())
	}
}

func TestARunOfRefusedTextsPausesTheProviderLikeAnAccountRefusal(t *testing.T) {
	tests := []struct {
		name    string
		refused int
		paused  bool
	}{
		{"no refused text", 0, false},
		{"one refused text is about that text", 1, false},
		{"a second refused text in the batch points at the request", QueryRefusalsBeforePause, true},
		{"more refused texts keep the provider paused", QueryRefusalsBeforePause + 3, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pause, paused := PauseAfterQueryRefusals(ProviderOpenCage, tt.refused, at)
			if paused != tt.paused {
				t.Fatalf("PauseAfterQueryRefusals(%d) paused = %v, want %v", tt.refused, paused, tt.paused)
			}
			if !paused {
				return
			}
			if pause.Provider != ProviderOpenCage || pause.Reason != geo.ReasonQueriesRefused || !pause.Since.Equal(at) || !pause.Until.Equal(at.Add(AccountPause)) {
				t.Fatalf("PauseAfterQueryRefusals() = %+v, want an account pause for refused texts", pause)
			}
			if !pause.Valid() || RefusalOf(geo.Unavailable(pause.Reason, 0)) != RefusalAccount {
				t.Fatalf("pause %+v must be a valid account refusal", pause)
			}
		})
	}
	if QueryRefusalsBeforePause < 2 {
		t.Fatal("a single refused text must never pause the provider")
	}
}

func TestAProviderPauseIsValidOnlyForAKnownProviderAnAccountRefusalAndAnOrderedSpan(t *testing.T) {
	valid, _ := PauseAfter(ProviderOpenCage, geo.Unavailable(geo.ReasonKeyRejected, 0), at)
	tests := []struct {
		name  string
		edit  func(p *ProviderPause)
		valid bool
	}{
		{"an opened pause", func(*ProviderPause) {}, true},
		{"every account refusal", func(p *ProviderPause) { p.Reason = geo.ReasonAccountRefused }, true},
		{"an unknown provider", func(p *ProviderPause) { p.Provider = "nominatim" }, false},
		{"no provider", func(p *ProviderPause) { p.Provider = "" }, false},
		{"a reason that is not an account refusal", func(p *ProviderPause) { p.Reason = geo.ReasonProviderDown }, false},
		{"a refused text", func(p *ProviderPause) { p.Reason = geo.ReasonQueryRefused }, false},
		{"a foreign reason", func(p *ProviderPause) { p.Reason = "maintenance" }, false},
		{"no reason", func(p *ProviderPause) { p.Reason = "" }, false},
		{"no start", func(p *ProviderPause) { p.Since = time.Time{} }, false},
		{"no end", func(p *ProviderPause) { p.Until = time.Time{} }, false},
		{"an end before the start", func(p *ProviderPause) { p.Until = p.Since.Add(-time.Minute) }, false},
		{"an end at the start", func(p *ProviderPause) { p.Until = p.Since }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := valid
			tt.edit(&p)
			if got := p.Valid(); got != tt.valid {
				t.Fatalf("Valid(%+v) = %v, want %v", p, got, tt.valid)
			}
		})
	}
}

func TestTheLongerOfTwoPausesHolds(t *testing.T) {
	short, _ := PauseAfter(ProviderOpenCage, geo.Unavailable(geo.ReasonKeyRejected, 0), at)
	long, _ := PauseAfter(ProviderOpenCage, geo.Unavailable(geo.ReasonAccountQuotaSpent, 5*time.Hour), at)
	tests := []struct {
		name string
		a, b ProviderPause
		want ProviderPause
	}{
		{"the later end wins", short, long, long},
		{"in either order", long, short, long},
		{"no pause yields the other", ProviderPause{}, short, short},
		{"and the other way round", short, ProviderPause{}, short},
		{"two absent pauses stay absent", ProviderPause{}, ProviderPause{}, ProviderPause{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := LongerPause(tt.a, tt.b); got != tt.want {
				t.Fatalf("LongerPause() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
