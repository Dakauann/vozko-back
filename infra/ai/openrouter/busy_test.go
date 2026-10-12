package openrouter

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"vozko/domain/ai"
)

const inFlightRefusal = `{"error":{"code":402,"message":"This request would exceed your available credits given your current in-flight requests. Retry after in-flight requests settle, or add credits.","metadata":%s}}`

const inFlightMetadata = `{"headers":{"Retry-After":"120"},"limit_source":"openrouter_in_flight_budget","reason":"in_flight_budget_exhausted","remedy_hint":"Retry after in-flight requests settle, or add credits."}`

func refusalServer(body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusPaymentRequired)
		fmt.Fprint(w, body)
	}))
}

func TestAnInFlightBudgetRefusalIsBusyNotTransientAndBillsNothing(t *testing.T) {
	srv := refusalServer(fmt.Sprintf(inFlightRefusal, inFlightMetadata))
	defer srv.Close()

	pub := &capturingPub{}
	failure := streamFailureOf(t, srv.URL, pub)
	if !errors.Is(failure, ai.ErrProviderBusy) || errors.Is(failure, ai.ErrProviderTransient) {
		t.Fatalf("error = %v, want a busy provider that is not retried as a passing fault", failure)
	}
	if wait, ok := ai.BusyRetryAfter(failure); !ok || wait != 120*time.Second {
		t.Fatalf("retry after = %v %v, want 120s", wait, ok)
	}
	if got := len(pub.events()); got != 0 {
		t.Fatalf("a refused call generated nothing to bill, got %d events", got)
	}
}

func TestAPlainOutOfCreditsRefusalIsNeitherBusyNorTransient(t *testing.T) {
	srv := refusalServer(`{"error":{"code":402,"message":"This request would exceed your available credits given your current in-flight requests. Retry after in-flight requests settle, or add credits."}}`)
	defer srv.Close()

	failure := streamFailureOf(t, srv.URL, &capturingPub{})
	if failure == nil || errors.Is(failure, ai.ErrProviderBusy) || errors.Is(failure, ai.ErrProviderTransient) {
		t.Fatalf("error = %v, want the plain permanent refusal", failure)
	}
}

func TestAnInFlightBudgetRefusalOnTheBlockingCallIsBusyToo(t *testing.T) {
	srv := refusalServer(fmt.Sprintf(inFlightRefusal, inFlightMetadata))
	defer srv.Close()

	_, err := newTestService(srv.URL, &capturingPub{}).Generate(context.Background(), ai.GenerateInput{
		Model: "x-ai/grok-4.7", WorkspaceID: "ws-1",
		Messages: []ai.Message{{Role: ai.RoleUser, Content: "oi"}},
	})
	if wait, ok := ai.BusyRetryAfter(err); !ok || wait != 120*time.Second {
		t.Fatalf("error = %v, want a busy provider with 120s to wait", err)
	}
}
