package openroutergen

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"vozko/domain/ai"
	"vozko/domain/mediagen"
)

const inFlightRefusalBody = `{"error":{"code":402,"message":"This request would exceed your available credits given your current in-flight requests. Retry after in-flight requests settle, or add credits.","metadata":{"headers":{"Retry-After":"120"},"limit_source":"openrouter_in_flight_budget","reason":"in_flight_budget_exhausted","remedy_hint":"Retry after in-flight requests settle, or add credits.%s"}}}`

func refusingServer(t *testing.T, body string, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusPaymentRequired)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestAnInFlightBudgetRefusalFailsEveryGeneratorAsBusyUnchargedAndUnsent(t *testing.T) {
	padding := strings.Repeat(" ", 2*maxErrorBody)
	generators := map[string]func(t *testing.T, srv *httptest.Server) error{
		"image": func(t *testing.T, srv *httptest.Server) error {
			g, err := NewImageGenerator(Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})
			if err != nil {
				t.Fatal(err)
			}
			_, err = g.Generate(context.Background(), request(mediagen.AspectSquare), nil)
			return err
		},
		"music": func(t *testing.T, srv *httptest.Server) error {
			_, err := audioGenerator(t, srv, &recordingEncoder{}).Generate(context.Background(), musicRequest(), nil)
			return err
		},
		"sound": func(t *testing.T, srv *httptest.Server) error {
			_, err := soundGenerator(t, srv, &recordingSoundEncoder{}).Generate(context.Background(), soundRequest(), nil)
			return err
		},
	}
	for name, generate := range generators {
		var hits atomic.Int32
		err := generate(t, refusingServer(t, fmt.Sprintf(inFlightRefusalBody, padding), &hits))
		var charged *mediagen.ChargedFailure
		if !errors.Is(err, ai.ErrProviderBusy) || !errors.Is(err, mediagen.ErrGenerationFailed) || errors.As(err, &charged) {
			t.Errorf("%s: error = %v, want an uncharged busy generation failure", name, err)
		}
		if wait, _ := ai.BusyRetryAfter(err); wait != 120*time.Second {
			t.Errorf("%s: retry after = %v, want 120s", name, wait)
		}
		if hits.Load() != 1 {
			t.Errorf("%s: a refused request must never be sent again, sent %d times", name, hits.Load())
		}
	}
}

func TestAPlainOutOfCreditsRefusalIsNotBusy(t *testing.T) {
	var hits atomic.Int32
	srv := refusingServer(t, `{"error":{"code":402,"message":"Insufficient credits"}}`, &hits)
	g, err := NewImageGenerator(Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	_, err = g.Generate(context.Background(), request(mediagen.AspectSquare), nil)
	if errors.Is(err, ai.ErrProviderBusy) || !errors.Is(err, mediagen.ErrGenerationFailed) || !strings.Contains(err.Error(), "status 402") {
		t.Fatalf("error = %v, want the plain generation failure", err)
	}
}
