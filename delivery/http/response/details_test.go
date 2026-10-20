package response

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestErrorsCanCarryStructuredDetails(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteErrorWithDetails(rec, http.StatusUnprocessableEntity, "whatsapp_form_invalid", "fix the form", map[string]any{"problems": []string{"a"}})
	var body struct {
		Error   bool                `json:"error"`
		Code    string              `json:"code"`
		Details map[string][]string `json:"details"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusUnprocessableEntity || !body.Error || body.Code != "whatsapp_form_invalid" || body.Details["problems"][0] != "a" {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
}

func TestErrorsWithoutDetailsKeepTheirShape(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteErrorWithCode(rec, http.StatusConflict, "x", "y", nil)
	if got := rec.Body.String(); got != "{\"error\":true,\"code\":\"x\",\"message\":\"y\"}\n" {
		t.Fatalf("body = %q", got)
	}
}
