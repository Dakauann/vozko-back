package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIfMatchVersion(t *testing.T) {
	cases := []struct {
		name    string
		header  string
		version int64
		ok      bool
	}{
		{"plain number", "3", 3, true},
		{"quoted etag", `"7"`, 7, true},
		{"spaces around", `  "12" `, 12, true},
		{"missing", "", 0, false},
		{"zero", "0", 0, false},
		{"negative", "-2", 0, false},
		{"not a number", `"abc"`, 0, false},
		{"weak etag", `W/"3"`, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPatch, "/x", nil)
			if tc.header != "" {
				req.Header.Set("If-Match", tc.header)
			}
			rec := httptest.NewRecorder()
			version, ok := IfMatchVersion(rec, req)
			if ok != tc.ok || version != tc.version {
				t.Fatalf("got %d %v, want %d %v", version, ok, tc.version, tc.ok)
			}
			if tc.ok {
				if rec.Body.Len() != 0 {
					t.Fatalf("an accepted version must not write: %s", rec.Body.String())
				}
				return
			}
			var body struct {
				Code string `json:"code"`
			}
			if rec.Code != http.StatusPreconditionRequired || json.Unmarshal(rec.Body.Bytes(), &body) != nil || body.Code != CodeVersionRequired {
				t.Fatalf("want 428 %s, got %d %s", CodeVersionRequired, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestWriteVersionConflict(t *testing.T) {
	type record struct {
		ID      string `json:"id"`
		Version int64  `json:"version"`
	}
	rec := httptest.NewRecorder()
	WriteVersionConflict(rec, "Salvo em outro lugar", record{ID: "p-1", Version: 4})

	var body struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Current record `json:"current"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusConflict || body.Code != CodeVersionConflict || body.Message != "Salvo em outro lugar" || body.Current.Version != 4 {
		t.Fatalf("got %d %+v", rec.Code, body)
	}
}
