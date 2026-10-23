package whatsapptemplate

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	buttonactionhttp "vozko/delivery/http/buttonaction"
	workspace_domain "vozko/domain/workspace"
)

func passThrough(_ workspace_domain.Resource, _ workspace_domain.Action, h http.HandlerFunc) http.HandlerFunc {
	return h
}

func TestTheButtonActionKindsAreNotTakenForATemplateID(t *testing.T) {
	router := mux.NewRouter()
	RegisterProtectedRoutes(router, &WhatsAppTemplateHandler{}, passThrough)
	buttonactionhttp.RegisterProtectedRoutes(router, buttonactionhttp.NewHandler(nil), passThrough)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/whatsapp/button-action-kinds", nil))

	if !strings.Contains(rec.Body.String(), "button_action_unavailable") {
		t.Fatalf("the request reached another route: status = %d, body = %s", rec.Code, rec.Body)
	}
}
