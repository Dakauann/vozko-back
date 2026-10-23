package emailpricing

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"vozko/delivery/http/response/responsetest"
	"vozko/domain/balance"
	"vozko/infra/http/middleware"
	"vozko/pkg/codegolden"
	balance_usecase "vozko/usecases/balance"
)

type priceListFunc func(context.Context, string) (balance_usecase.EmailPriceListResult, error)

func (f priceListFunc) Execute(ctx context.Context, ws string) (balance_usecase.EmailPriceListResult, error) {
	return f(ctx, ws)
}

func withWorkspace(r *http.Request, ws string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), middleware.WorkspaceIDContextKey, ws))
}

func TestGetReturnsTheResolvedPrices(t *testing.T) {
	var asked string
	h := NewHandler(priceListFunc(func(_ context.Context, ws string) (balance_usecase.EmailPriceListResult, error) {
		asked = ws
		return balance_usecase.EmailPriceListResult{ExchangeRate: 6, Send: balance_usecase.EmailPrice{Service: "send", UnitMicros: 583},
			Receive:         balance_usecase.EmailReceivePrices{PerMessage: balance_usecase.EmailPrice{Service: "receive", Metric: "per_message", UnitMicros: 583}, PerChunk: balance_usecase.EmailPrice{Service: "receive", Metric: "per_chunk", UnitMicros: 315}, ChunkBytes: 262144},
			SendAttachments: balance_usecase.EmailChunkPrices{PerChunk: balance_usecase.EmailPrice{Service: "send", Metric: "per_chunk", UnitMicros: 102}, ChunkBytes: 262144}}, nil
	}))
	rec := httptest.NewRecorder()
	h.Get(rec, withWorkspace(httptest.NewRequest(http.MethodGet, "/email/pricing", nil), "ws-1"))
	if rec.Code != http.StatusOK || asked != "ws-1" {
		t.Fatalf("status %d asked %q", rec.Code, asked)
	}
	var body EmailPricingResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Send.UnitMicros != 583 || body.ExchangeRate != 6 ||
		body.Receive.PerMessage.UnitMicros != 583 || body.Receive.PerChunk.UnitMicros != 315 || body.Receive.PerChunk.Metric != "per_chunk" || body.Receive.ChunkBytes != 262144 ||
		body.SendAttachments.PerChunk.UnitMicros != 102 || body.SendAttachments.PerChunk.Service != "send" || body.SendAttachments.ChunkBytes != 262144 {
		t.Fatalf("body %s", rec.Body.String())
	}
}

func TestGetWithoutAWorkspaceIsRefused(t *testing.T) {
	h := NewHandler(priceListFunc(func(context.Context, string) (balance_usecase.EmailPriceListResult, error) {
		t.Fatal("no workspace, no lookup")
		return balance_usecase.EmailPriceListResult{}, nil
	}))
	rec := httptest.NewRecorder()
	h.Get(rec, httptest.NewRequest(http.MethodGet, "/email/pricing", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestEveryPricingErrorWritesItsCode(t *testing.T) {
	responsetest.RequireEveryErrorWritesItsCode(t, pricingRefusals, func(w http.ResponseWriter, err error) {
		h := NewHandler(priceListFunc(func(context.Context, string) (balance_usecase.EmailPriceListResult, error) {
			return balance_usecase.EmailPriceListResult{}, err
		}))
		h.Get(w, withWorkspace(httptest.NewRequest(http.MethodGet, "/email/pricing", nil), "ws-1"))
	})
	rec := httptest.NewRecorder()
	h := NewHandler(priceListFunc(func(context.Context, string) (balance_usecase.EmailPriceListResult, error) {
		return balance_usecase.EmailPriceListResult{}, errors.New("db down")
	}))
	h.Get(rec, withWorkspace(httptest.NewRequest(http.MethodGet, "/email/pricing", nil), "ws-1"))
	if rec.Code != http.StatusInternalServerError || responsetest.CodeOf(t, rec) != refusalPricingFailed.Code {
		t.Fatalf("unknown failure answered %d %s", rec.Code, rec.Body.String())
	}
	wrapped := httptest.NewRecorder()
	h = NewHandler(priceListFunc(func(context.Context, string) (balance_usecase.EmailPriceListResult, error) {
		return balance_usecase.EmailPriceListResult{}, errors.Join(errors.New("ctx"), balance.ErrPriceUnavailable)
	}))
	h.Get(wrapped, withWorkspace(httptest.NewRequest(http.MethodGet, "/email/pricing", nil), "ws-1"))
	if responsetest.CodeOf(t, wrapped) != "email_pricing_unavailable" {
		t.Fatalf("wrapped price error answered %s", wrapped.Body.String())
	}
}

func TestPricingErrorCodesMatchTheGolden(t *testing.T) {
	codes := RefusalCodes()
	codegolden.Compare(t, filepath.Join("testdata", "error_codes.golden"), codes, "update testdata/error_codes.golden and translate emailMarketing.errors.<code> in the pt, en, es and de catalogs of vozko-front")
}
