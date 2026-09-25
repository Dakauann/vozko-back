package unofficial_whatsapp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	"vozko/domain/conversation"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	"vozko/infra/http/middleware"
	uwcuc "vozko/usecases/unofficial_whatsapp_campaign"
)

type openScope struct{}

func (openScope) GetDepartmentScope(string, string, bool) (conversation.DepartmentAccessScope, bool) {
	return conversation.DepartmentAccessScope{}, true
}

type foreignCampaign struct{}

func (foreignCampaign) Execute(_ context.Context, id string) (*uwc.Campaign, error) {
	return &uwc.Campaign{ID: id, WorkspaceID: "ws-2"}, nil
}

type dispatchRecorder struct{ calls int }

func (d *dispatchRecorder) Dispatch(context.Context, uwc.DispatchCampaignInput) error {
	d.calls++
	return nil
}

func campaignRequest(method, path string, handler http.HandlerFunc) *httptest.ResponseRecorder {
	router := mux.NewRouter()
	router.HandleFunc("/unofficial-whatsapp/campaigns/{id}/start", handler).Methods(method)
	router.HandleFunc("/unofficial-whatsapp/campaigns/{id}", handler).Methods(method)
	r := httptest.NewRequest(method, path, nil)
	ctx := context.WithValue(r.Context(), middleware.WorkspaceIDContextKey, "ws-1")
	ctx = context.WithValue(ctx, middleware.ClaimsContextKey, &auth.Claims{UserID: "u-1", Role: "member"})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r.WithContext(ctx))
	return w
}

func TestCampaignRoutesRefuseAnotherWorkspacesCampaign(t *testing.T) {
	dispatch := &dispatchRecorder{}
	h := NewCampaignHandler(CampaignHandlerDeps{
		Get: foreignCampaign{}, Access: uwcuc.NewCampaignAccessUseCase(foreignCampaign{}),
		Dispatch: dispatch, Departments: openScope{},
	})
	if w := campaignRequest(http.MethodPost, "/unofficial-whatsapp/campaigns/c-9/start", h.Start); w.Code != http.StatusNotFound || dispatch.calls != 0 {
		t.Fatalf("start: status %d dispatches %d", w.Code, dispatch.calls)
	}
	if w := campaignRequest(http.MethodGet, "/unofficial-whatsapp/campaigns/c-9", h.Get); w.Code != http.StatusNotFound {
		t.Fatalf("get: status %d", w.Code)
	}
}

func TestCampaignRoutesRefuseWithoutADepartmentResolver(t *testing.T) {
	dispatch := &dispatchRecorder{}
	h := NewCampaignHandler(CampaignHandlerDeps{Access: uwcuc.NewCampaignAccessUseCase(foreignCampaign{}), Dispatch: dispatch})
	if w := campaignRequest(http.MethodPost, "/unofficial-whatsapp/campaigns/c-9/start", h.Start); w.Code != http.StatusForbidden || dispatch.calls != 0 {
		t.Fatalf("status %d dispatches %d", w.Code, dispatch.calls)
	}
}

