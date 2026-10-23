package emailhygiene

import (
	"errors"
	"testing"
	"time"

	es "vozko/domain/emailsending"
)

func TestOneToOneSendsFollowTheCampaignRuleAndTheAPIStaysLenient(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	accepts := Standing{Domain: DomainAccepts}
	cases := map[string]struct {
		facts    Facts
		standing Standing
		want     string
	}{
		"accepting domain":     {Facts{}, accepts, ""},
		"domain being checked": {Facts{}, Standing{Domain: DomainPending}, CodeDomainChecking},
		"domain never checked": {Facts{}, Standing{}, CodeDomainChecking},
		"unknown domain":       {Facts{}, Standing{Domain: DomainUnknown}, CodeDomainUnknown},
		"disposable address":   {Facts{Disposable: true}, accepts, CodeDisposableAddress},
		"domain without mail":  {Facts{}, Standing{Domain: DomainNoMail}, CodeNoMailDomain},
		"role address":         {Facts{Role: true}, accepts, ""},
		"typo suggestion":      {Facts{Suggestion: "gmail.com"}, accepts, ""},
		"suppressed address":   {Facts{}, Standing{Suppression: es.SuppressComplaint, Domain: DomainUnknown}, ""},
		"known bad address":    {Facts{}, Standing{BadAddress: true, Domain: DomainUnknown}, ""},
	}
	for _, kind := range []es.RunKind{es.RunReply, es.RunTriggered, es.RunTest, es.RunKind("outreach")} {
		for name, c := range cases {
			t.Run(string(kind)+" "+name, func(t *testing.T) {
				got := OneToOneRefusal(kind, c.facts, c.standing, now)
				if c.want == "" {
					if got != nil {
						t.Fatalf("refused with %q, want sent", got.Code)
					}
					return
				}
				if got == nil || got.Code != c.want {
					t.Fatalf("got %+v, want %s", got, c.want)
				}
			})
		}
	}
	for _, kind := range []es.RunKind{es.RunAPI, es.RunCampaign} {
		for name, c := range cases {
			if got := OneToOneRefusal(kind, c.facts, c.standing, now); got != nil {
				t.Fatalf("%s %s refused with %q; the API keeps its lenient rule and campaigns filter before dispatch", kind, name, got.Code)
			}
		}
	}
}

func TestARecipientRefusalCarriesItsCodeAndOnlyCheckingIsRetryable(t *testing.T) {
	err := error(&RecipientRefusal{Code: CodeDomainChecking})
	wrapped := errors.Join(errors.New("context"), err)
	if code, ok := RecipientRefusalCode(wrapped); !ok || code != CodeDomainChecking {
		t.Fatalf("code = %q ok = %v", code, ok)
	}
	if !errors.Is(wrapped, ErrRecipientRefused) || !RetryableRefusal(wrapped) {
		t.Fatal("a domain still being checked is a retryable recipient refusal")
	}
	if RetryableRefusal(&RecipientRefusal{Code: CodeDisposableAddress}) {
		t.Fatal("a disposable address never becomes sendable by waiting")
	}
	if _, ok := RecipientRefusalCode(errors.New("other")); ok {
		t.Fatal("another error carries no hygiene code")
	}
}

func TestOnlyAnUncheckedOrStaleDomainAsksForACheck(t *testing.T) {
	for st, want := range map[Standing]bool{
		{}:                      true,
		{Domain: DomainPending}: true,
		{Domain: DomainAccepts, DomainStale: true}: true,
		{Domain: DomainAccepts}:                    false,
		{Domain: DomainUnknown}:                    false,
	} {
		if got := st.NeedsDomainCheck(); got != want {
			t.Errorf("%+v NeedsDomainCheck = %v, want %v", st, got, want)
		}
	}
}
