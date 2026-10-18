package httpx

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"vozko/domain/auth"
	"vozko/domain/workspace"
)

func TestRequesterOfReadsTheCallerAndTheSystemAdminRole(t *testing.T) {
	cases := []struct {
		name    string
		request *http.Request
		want    workspace.Requester
	}{
		{"a member", requestAs(&auth.Claims{UserID: " u1 ", Role: "user"}, "ws"), workspace.Requester{UserID: "u1"}},
		{"a system admin", requestAs(&auth.Claims{UserID: "root", Role: "admin"}, "ws"), workspace.Requester{UserID: "root", SystemAdmin: true}},
		{"nobody", requestAs(nil, "ws"), workspace.Requester{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RequesterOf(tc.request); got != tc.want {
				t.Fatalf("RequesterOf = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestWriteConsentGrantRefusalAnswers403WithItsCode(t *testing.T) {
	rec := httptest.NewRecorder()
	if !WriteConsentGrantRefusal(rec, fmt.Errorf("saving: %w", workspace.ErrConsentGrantForbidden)) {
		t.Fatal("the consent refusal was not written")
	}
	var body struct {
		Code     string            `json:"code"`
		Expected map[string]string `json:"expected"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusForbidden || body.Code != "lead_consent_grant_forbidden" || body.Expected["permission"] != "leads:consent" {
		t.Fatalf("status %d body %+v", rec.Code, body)
	}
	if WriteConsentGrantRefusal(httptest.NewRecorder(), workspace.ErrUnauthorized) {
		t.Fatal("another error is not a consent refusal")
	}
}
