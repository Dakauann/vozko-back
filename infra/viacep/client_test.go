package viacep

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"vozko/domain/cep"
)

func serve(t *testing.T, handler http.HandlerFunc) (*Client, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	return NewClient(Config{BaseURL: server.URL + "/ws", Timeout: 200 * time.Millisecond}), &calls
}

func TestLookupReturnsTheAddressWithItsCityCode(t *testing.T) {
	var path string
	client, _ := serve(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"cep":"01310-100","logradouro":"Avenida Paulista","complemento":"de 612 a 1510 - lado par","bairro":"Bela Vista","localidade":"São Paulo","uf":"SP","ibge":"3550308","gia":"1004","ddd":"11","siafi":"7107"}`))
	})

	info, err := client.Lookup(context.Background(), "01310-100")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "/ws/01310100/json/" {
		t.Fatalf("requested %q", path)
	}
	expected := cep.CEPInfo{Cep: "01310100", Logradouro: "Avenida Paulista", Complement: "de 612 a 1510 - lado par", Bairro: "Bela Vista", Localidade: "São Paulo", Uf: "SP", IBGE: "3550308"}
	if *info != expected {
		t.Fatalf("Lookup() = %+v, expected %+v", *info, expected)
	}
}

func TestLookupDropsAMalformedCityCode(t *testing.T) {
	client, _ := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"cep":"01310-100","localidade":"São Paulo","uf":"SP","ibge":"35503"}`))
	})
	info, err := client.Lookup(context.Background(), "01310100")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.IBGE != "" {
		t.Fatalf("a malformed city code must not be kept, got %q", info.IBGE)
	}
}

func TestLookupRefusesAnInvalidCEPWithoutCallingViaCEP(t *testing.T) {
	client, calls := serve(t, func(w http.ResponseWriter, _ *http.Request) {})
	for _, raw := range []string{"0131010", "../01310100", "01310100/../x", ""} {
		if _, err := client.Lookup(context.Background(), raw); !errors.Is(err, cep.ErrInvalidCEP) {
			t.Fatalf("Lookup(%q): expected ErrInvalidCEP, got %v", raw, err)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("ViaCEP was called %d times", calls.Load())
	}
}

func TestLookupErrors(t *testing.T) {
	tests := []struct {
		name     string
		handler  http.HandlerFunc
		expected error
	}{
		{"an unknown CEP flagged with a boolean", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"erro": true}`)) }, cep.ErrNotFound},
		{"an unknown CEP flagged with a string", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"erro": "true"}`)) }, cep.ErrNotFound},
		{"a format ViaCEP refuses", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadRequest) }, cep.ErrInvalidCEP},
		{"a server error", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) }, cep.ErrUnavailable},
		{"rate limited", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTooManyRequests) }, cep.ErrUnavailable},
		{"a body that is not JSON", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`<html>maintenance</html>`)) }, cep.ErrUnavailable},
		{"a body without a city", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{}`)) }, cep.ErrUnavailable},
		{"a slow server past the timeout", func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(2 * time.Second):
			}
		}, cep.ErrUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, _ := serve(t, tt.handler)
			started := time.Now()
			info, err := client.Lookup(context.Background(), "01310100")
			if !errors.Is(err, tt.expected) || info != nil {
				t.Fatalf("expected %v and no info, got %+v, %v", tt.expected, info, err)
			}
			if time.Since(started) > time.Second {
				t.Fatalf("the lookup took %v, the timeout was not applied", time.Since(started))
			}
		})
	}
}

func TestLookupReportsTheUpstreamStatus(t *testing.T) {
	client, _ := serve(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) })
	_, err := client.Lookup(context.Background(), "01310100")
	var status *StatusError
	if !errors.As(err, &status) || status.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected a StatusError with 503, got %v", err)
	}
}

func TestLookupStopsWhenTheCallerGivesUp(t *testing.T) {
	client, _ := serve(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Lookup(ctx, "01310100"); !errors.Is(err, cep.ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
}
