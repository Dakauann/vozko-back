package openrouterlimit

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"vozko/domain/ai"
)

var refusedCall = errors.New("status 402")

func inFlightMetadata(headers map[string]any) map[string]any {
	return map[string]any{
		"headers":      headers,
		"limit_source": "openrouter_in_flight_budget",
		"reason":       "in_flight_budget_exhausted",
		"remedy_hint":  "Retry after in-flight requests settle, or add credits.",
	}
}

func TestAnInFlightBudgetRefusalIsBusyAndCarriesItsRetryAfter(t *testing.T) {
	err := Refusal(inFlightMetadata(map[string]any{"Retry-After": "120"}), nil, refusedCall)
	if !errors.Is(err, ai.ErrProviderBusy) || !errors.Is(err, refusedCall) {
		t.Fatalf("error = %v, want a busy provider wrapping the refusal", err)
	}
	if wait, ok := ai.BusyRetryAfter(err); !ok || wait != 120*time.Second {
		t.Fatalf("retry after = %v %v, want 120s", wait, ok)
	}
}

func TestTheRetryAfterHeaderNameIsCaseInsensitiveAndMayBeANumber(t *testing.T) {
	err := Refusal(inFlightMetadata(map[string]any{"retry-after": float64(45)}), nil, refusedCall)
	if wait, _ := ai.BusyRetryAfter(err); wait != 45*time.Second {
		t.Fatalf("retry after = %v, want 45s", wait)
	}
}

func TestTheRealResponseHeaderWinsOverTheEchoedOne(t *testing.T) {
	header := http.Header{}
	header.Set("Retry-After", "30")
	err := Refusal(inFlightMetadata(map[string]any{"Retry-After": "120"}), header, refusedCall)
	if wait, _ := ai.BusyRetryAfter(err); wait != 30*time.Second {
		t.Fatalf("retry after = %v, want the response header's 30s", wait)
	}
}

func TestAMissingOrUnreadableRetryAfterIsUnknownButStillBusy(t *testing.T) {
	for _, headers := range []map[string]any{nil, {"Retry-After": "soon"}, {"Retry-After": "-5"}} {
		err := Refusal(inFlightMetadata(headers), nil, refusedCall)
		wait, ok := ai.BusyRetryAfter(err)
		if !ok || wait != 0 {
			t.Errorf("headers %v: retry after = %v %v, want busy with no known wait", headers, wait, ok)
		}
	}
}

func TestOnlyTheFullInFlightSignatureIsBusy(t *testing.T) {
	cases := []map[string]any{
		nil,
		{},
		{"reason": "in_flight_budget_exhausted"},
		{"limit_source": "openrouter_in_flight_budget"},
		{"reason": "insufficient_credits", "limit_source": "openrouter_in_flight_budget"},
		{"reason": "in_flight_budget_exhausted", "limit_source": "key_limit"},
	}
	for _, metadata := range cases {
		if err := Refusal(metadata, nil, refusedCall); err != nil {
			t.Errorf("metadata %v: got %v, want no busy refusal", metadata, err)
		}
	}
}

func TestARefusalBodyIsReadFromItsStructuredMetadata(t *testing.T) {
	body := `{"error":{"code":402,"message":"This request would exceed your available credits given your current in-flight requests.","metadata":{"headers":{"Retry-After":"120"},"limit_source":"openrouter_in_flight_budget","reason":"in_flight_budget_exhausted"}}}`
	err := RefusalInBody([]byte(body), nil, refusedCall)
	if wait, ok := ai.BusyRetryAfter(err); !ok || wait != 120*time.Second {
		t.Fatalf("retry after = %v %v", wait, ok)
	}
}

func TestTheHumanMessageAloneNeverMakesARefusalBusy(t *testing.T) {
	bodies := []string{
		`{"error":{"code":402,"message":"This request would exceed your available credits given your current in-flight requests. Retry after in-flight requests settle, or add credits."}}`,
		`{"error":{"code":402,"message":"Insufficient credits"}}`,
		`not json`,
		``,
	}
	for _, body := range bodies {
		if err := RefusalInBody([]byte(body), nil, refusedCall); err != nil {
			t.Errorf("body %q: got %v, want no busy refusal", body, err)
		}
	}
}

func TestTheBusyErrorNamesTheReasonForOpsWithoutTheRawCause(t *testing.T) {
	secret := errors.New("Authorization: Bearer sk-or-secret")
	text := Refusal(inFlightMetadata(map[string]any{"Retry-After": "120"}), nil, secret).Error()
	for _, want := range []string{"in_flight_budget_exhausted", "openrouter_in_flight_budget", "2m0s", "credit"} {
		if !strings.Contains(text, want) {
			t.Errorf("log text %q is missing %q", text, want)
		}
	}
	if strings.Contains(text, "sk-or-secret") {
		t.Fatalf("the busy error must not repeat the raw cause, got %q", text)
	}
}
