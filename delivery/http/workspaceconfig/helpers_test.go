package workspaceconfig

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"vozko/domain/auth"
)

func codeOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return body.Code
}

var owner = &auth.Claims{UserID: "owner-1", Role: "user"}
