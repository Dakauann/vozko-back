package templatehealthhttp

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"vozko/domain/whatsapp/quality"
	domain "vozko/domain/whatsapp/template"
	template_usecase "vozko/usecases/whatsapp/template"
)

type readerStub struct {
	health template_usecase.TemplateHealth
	err    error
	asked  string
}

func (r *readerStub) Read(workspaceID, templateID string) (template_usecase.TemplateHealth, error) {
	r.asked = workspaceID + "/" + templateID
	return r.health, r.err
}

func serve(reader *readerStub) *httptest.ResponseRecorder {
	router := mux.NewRouter()
	router.HandleFunc("/whatsapp/templates/{id}/health", NewHandler(reader).Get).Methods(http.MethodGet)
	req := httptest.NewRequest(http.MethodGet, "/whatsapp/templates/tpl/health", nil)
	req.Header.Set("X-Workspace-ID", "ws")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestHealthShowsPendingQualityWhenMetaHasNotRated(t *testing.T) {
	reader := &readerStub{health: template_usecase.TemplateHealth{TemplateID: "tpl", Status: domain.TemplateStatusApproved, Quality: quality.QualityUnknown}}

	rec := serve(reader)

	var body Response
	if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &body) != nil {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if body.Quality != "UNKNOWN" || body.QualityAt != nil || body.Changes == nil || len(body.Changes) != 0 || reader.asked != "ws/tpl" {
		t.Fatalf("body %+v asked %q", body, reader.asked)
	}
}

func TestHealthCarriesTheRecentChanges(t *testing.T) {
	at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	reader := &readerStub{health: template_usecase.TemplateHealth{
		TemplateID: "tpl", Status: domain.TemplateStatusApproved, Quality: "YELLOW", QualityAt: at, Flagged: true,
		Changes: []quality.Change{{Signal: quality.SignalQuality, Previous: "GREEN", Current: "YELLOW", OccurredAt: at}},
	}}

	var body Response
	_ = json.Unmarshal(serve(reader).Body.Bytes(), &body)

	if body.Quality != "YELLOW" || body.QualityAt == nil || !body.Flagged || len(body.Changes) != 1 || body.Changes[0].Current != "YELLOW" || body.Changes[0].Signal != "quality" {
		t.Fatalf("body %+v", body)
	}
}

func TestHealthRefusesAWorkspaceWithoutAGrant(t *testing.T) {
	if rec := serve(&readerStub{err: domain.ErrTemplateAccessDenied}); rec.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403", rec.Code)
	}
	if rec := serve(&readerStub{err: domain.ErrTemplateNotFound}); rec.Code != http.StatusNotFound {
		t.Fatalf("status %d, want 404", rec.Code)
	}
	if rec := serve(&readerStub{err: errors.New("db down")}); rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", rec.Code)
	}
}
