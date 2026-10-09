package opportunity

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vozko/domain/auth"
	"vozko/domain/customfield"
	opportunitydomain "vozko/domain/opportunity"
	"vozko/domain/shared"
	"vozko/infra/http/middleware"
	opportunity_usecase "vozko/usecases/opportunity"
)

type draftRecorder struct {
	opportunitydomain.PersonDealsUseCase
	by    shared.Person
	draft opportunitydomain.DealDraft
	err   error
}

func (d *draftRecorder) Create(by shared.Person, _ string, draft opportunitydomain.DealDraft) (*opportunitydomain.Opportunity, error) {
	d.by, d.draft = by, draft
	if d.err != nil {
		return nil, d.err
	}
	return &opportunitydomain.Opportunity{ID: "opp-1"}, nil
}

func createRequest(body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/opportunities", strings.NewReader(body))
	ctx := context.WithValue(r.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: "u1"})
	return r.WithContext(ctx)
}

func TestCreateGoesThroughThePersonsEntryAccess(t *testing.T) {
	deals := &draftRecorder{err: opportunitydomain.ErrEntryAccess}
	rec := httptest.NewRecorder()
	(&OpportunityHandler{deals: deals}).Create(rec, createRequest(`{"pipelineId":"p1","stageId":"s1","title":"Plano","ownerId":"u2","carteiraId":"c1","source":"indicacao","customFields":{"segmento":"x"},"linkEntryId":" e1 ","linkEntryType":"whatsapp"}`))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
	d := deals.draft
	if deals.by.UserID != "u1" || d.EntryID != "e1" || d.EntryType != "whatsapp" || d.OwnerID != "u2" || d.CarteiraID != "c1" || d.Source != "indicacao" || d.CustomFields["segmento"] != "x" {
		t.Fatalf("by %+v draft %+v", deals.by, d)
	}
}

func TestCreateRefusesWithoutThePersonDeals(t *testing.T) {
	rec := httptest.NewRecorder()
	(&OpportunityHandler{}).Create(rec, createRequest(`{"pipelineId":"p1","stageId":"s1","title":"Plano"}`))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestCustomFieldValueRefusalsCarryTheirCodes(t *testing.T) {
	for _, err := range []error{customfield.ErrUnknownKey, customfield.ErrValueType, customfield.ErrValueNotInOptions, customfield.ErrValueRequired} {
		rec := httptest.NewRecorder()
		(&OpportunityHandler{}).handleDomainError(rec, err)
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"code":"`+customfield.ErrorCode(err)+`"`) {
			t.Fatalf("%v: status = %d body = %s", err, rec.Code, rec.Body.String())
		}
	}
}

type openEntries struct{}

func (openEntries) CanAccessEntry(string, string, string, string, bool) bool { return true }

func TestCreateAnswersAMissingLinkEntryTypeWithBadRequest(t *testing.T) {
	deals := opportunity_usecase.NewPersonDeals(nil, openEntries{}, nil)
	rec := httptest.NewRecorder()
	(&OpportunityHandler{deals: deals}).Create(rec, createRequest(`{"pipelineId":"p1","stageId":"s1","title":"Plano","linkEntryId":"e1"}`))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body = %s, want 400", rec.Code, rec.Body.String())
	}
}
