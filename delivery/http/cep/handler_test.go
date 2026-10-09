package cep

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	cepdomain "vozko/domain/cep"
)

type fakeSearch struct {
	info *cepdomain.CEPInfo
	err  error
	ctx  context.Context
}

func (f *fakeSearch) Execute(ctx context.Context, _ string) (*cepdomain.CEPInfo, error) {
	f.ctx = ctx
	return f.info, f.err
}

func TestSearchAnswersTheAddressWithItsCityCode(t *testing.T) {
	search := &fakeSearch{info: &cepdomain.CEPInfo{Cep: "01310100", Logradouro: "Avenida Paulista", Bairro: "Bela Vista", Localidade: "São Paulo", Uf: "SP", IBGE: "3550308"}}
	rec := httptest.NewRecorder()
	NewCEPHandler(search).Search(rec, httptest.NewRequest(http.MethodGet, "/cep/search?cep=01310-100", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if search.ctx == nil {
		t.Fatal("the request context must reach the lookup")
	}
	var body CEPResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.IBGE != "3550308" || body.Localidade != "São Paulo" {
		t.Fatalf("unexpected body %s", rec.Body.String())
	}
}

func TestSearchMapsEachFailureToItsStatus(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		err      error
		expected int
	}{
		{"a missing CEP", "", nil, http.StatusBadRequest},
		{"an invalid CEP", "?cep=123", cepdomain.ErrInvalidCEP, http.StatusBadRequest},
		{"an unknown CEP", "?cep=01310100", cepdomain.ErrNotFound, http.StatusNotFound},
		{"the lookup is down", "?cep=01310100", cepdomain.ErrUnavailable, http.StatusServiceUnavailable},
		{"no lookup configured", "?cep=01310100", cepdomain.ErrLookupNotConfigured, http.StatusServiceUnavailable},
		{"anything else", "?cep=01310100", errors.New("db down"), http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			NewCEPHandler(&fakeSearch{err: tt.err}).Search(rec, httptest.NewRequest(http.MethodGet, "/cep/search"+tt.query, nil))
			if rec.Code != tt.expected {
				t.Fatalf("status %d, expected %d: %s", rec.Code, tt.expected, rec.Body.String())
			}
		})
	}
}
