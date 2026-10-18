package lead

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"vozko/domain/leadimport"
	workspace_domain "vozko/domain/workspace"
)

func TestAnImportThatWouldGrantConsentIsRefusedWithTheConsentCode(t *testing.T) {
	refusal := fmt.Errorf("%w: %w", leadimport.ErrForbidden, workspace_domain.ErrConsentGrantForbidden)
	routes := []struct {
		path string
		body any
	}{
		{"/leads/imports/" + importJobID + "/dry-run", map[string]any{"columns": []map[string]any{{"index": 0, "field": "number"}, {"index": 1, "field": "consent_date"}}}},
		{"/leads/imports/" + importJobID + "/start", nil},
	}
	for _, route := range routes {
		rec := send(t, importRouter(&stubImports{err: refusal}), http.MethodPost, route.path, nil, route.body)
		var body struct {
			Code     string            `json:"code"`
			Expected map[string]string `json:"expected"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if rec.Code != http.StatusForbidden || body.Code != "lead_consent_grant_forbidden" || body.Expected["permission"] != "leads:consent" {
			t.Fatalf("%s: status %d body %s", route.path, rec.Code, rec.Body.String())
		}
	}
}
