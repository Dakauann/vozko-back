package payment

import (
	"strings"
	"testing"
	"time"
)

func TestReversalWindowFollowsTheMethod(t *testing.T) {
	if got := ReversalWindow("PIX"); got < 80*24*time.Hour {
		t.Errorf("PIX can be returned through MED for 80 days, window is %v", got)
	}
	if got := ReversalWindow("CREDIT_CARD"); got < 180*24*time.Hour {
		t.Errorf("card chargebacks can arrive for 180 days, window is %v", got)
	}
	if got := ReversalWindow("BOLETO"); got != 0 {
		t.Errorf("boleto has no bank-side reversal, window is %v", got)
	}
	if got := ReversalWindow("SOMETHING_ELSE"); got < 180*24*time.Hour {
		t.Errorf("an unknown method must get the longest window, got %v", got)
	}
}

func TestAProviderWithOpenObligationsNeedsCredentials(t *testing.T) {
	obligations := []Obligations{
		{Provider: ProviderAsaas, Unsettled: 0, Reversible: 3},
		{Provider: ProviderMercadoPago, Unsettled: 2, Reversible: 0},
	}
	missing := ProvidersMissingCredentials(obligations, map[Provider]bool{ProviderMercadoPago: true})
	if len(missing) != 1 || missing[0].Provider != ProviderAsaas {
		t.Fatalf("Asaas has reversible charges and no credentials, got %+v", missing)
	}
}

func TestAProviderWithNothingOpenCanBeRetired(t *testing.T) {
	obligations := []Obligations{{Provider: ProviderAsaas}}
	if missing := ProvidersMissingCredentials(obligations, map[Provider]bool{}); len(missing) != 0 {
		t.Fatalf("a provider with nothing open needs no credentials, got %+v", missing)
	}
}

func TestEveryConfiguredProviderPasses(t *testing.T) {
	obligations := []Obligations{{Provider: ProviderAsaas, Unsettled: 1}, {Provider: ProviderMercadoPago, Reversible: 1}}
	configured := map[Provider]bool{ProviderAsaas: true, ProviderMercadoPago: true}
	if missing := ProvidersMissingCredentials(obligations, configured); len(missing) != 0 {
		t.Fatalf("every provider is configured, got %+v", missing)
	}
}

func TestObligationsDescribeThemselves(t *testing.T) {
	text := Obligations{Provider: ProviderAsaas, Unsettled: 2, Reversible: 5}.String()
	for _, part := range []string{"asaas", "2", "5"} {
		if !strings.Contains(text, part) {
			t.Errorf("%q must mention %q so the boot error is actionable", text, part)
		}
	}
}
