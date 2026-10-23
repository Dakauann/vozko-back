package whatsapptemplate

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/whatsapp/template"
)

func checkNames(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	router := mux.NewRouter()
	router.HandleFunc("/whatsapp/templates/variable-names/check", (&WhatsAppTemplateHandler{}).CheckVariableNames).Methods(http.MethodPost)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/whatsapp/templates/variable-names/check", strings.NewReader(body)))
	return rec
}

func expectedVariable(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Expected map[string]string `json:"expected"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return body.Expected["variable"]
}

func TestVariableNamesAreCheckedByTheTemplateRule(t *testing.T) {
	if rec := checkNames(t, `{"names":["primeiro_nome","pedido2"]}`); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"valid":true`) {
		t.Fatalf("valid names = %d %s", rec.Code, rec.Body)
	}
	rec := checkNames(t, `{"names":["nome","Bairro","x y"]}`)
	if rec.Code != http.StatusBadRequest || errorCodeOf(t, rec) != template.CodeParameterNameInvalid || expectedVariable(t, rec) != "Bairro" {
		t.Fatalf("invalid name = %d %s", rec.Code, rec.Body)
	}
	if rec := checkNames(t, `{`); rec.Code != http.StatusBadRequest {
		t.Fatalf("broken body = %d", rec.Code)
	}
}

func TestAnEditWithAnInvalidVariableNamesIt(t *testing.T) {
	rec := serveEdit(t, &fakeEditor{err: &template.ParameterNameError{Name: "Pedido"}}, `{"components":[{"type":"BODY","text":"Oi"}]}`, true)
	if rec.Code != http.StatusBadRequest || errorCodeOf(t, rec) != template.CodeParameterNameInvalid || expectedVariable(t, rec) != "Pedido" {
		t.Fatalf("edit = %d %s", rec.Code, rec.Body)
	}
}
