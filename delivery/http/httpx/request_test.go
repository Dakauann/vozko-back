package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type strictBody struct {
	Name *string `json:"name"`
}

func TestDecodeStrictJSONRefusesWhatTheRouteDoesNotTake(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		ok      bool
		mention string
	}{
		{"a known key", `{"name":"Ana"}`, true, ""},
		{"an empty object", `{}`, true, ""},
		{"an unknown key", `{"name":"Ana","number":"5511987654321"}`, false, "number"},
		{"a misspelled key", `{"nmae":"Ana"}`, false, "nmae"},
		{"not json", `{"name":`, false, ""},
		{"two documents", `{"name":"Ana"}{"name":"Bia"}`, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			var into strictBody
			if got := DecodeStrictJSON(rec, req, &into); got != tc.ok {
				t.Fatalf("DecodeStrictJSON = %v, want %v (%s)", got, tc.ok, rec.Body.String())
			}
			if tc.ok {
				if rec.Body.Len() != 0 {
					t.Fatalf("an accepted body must not write: %s", rec.Body.String())
				}
				return
			}
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
			var out struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.Code != CodeInvalidBody {
				t.Fatalf("body = %s", rec.Body.String())
			}
			if tc.mention != "" && !strings.Contains(out.Message, tc.mention) {
				t.Fatalf("the refusal must name %q, got %q", tc.mention, out.Message)
			}
		})
	}
}

func TestDecodeOptionalStrictJSONTakesNoBodyAndStillRefusesUnknownKeys(t *testing.T) {
	cases := []struct {
		name string
		body string
		ok   bool
		want string
	}{
		{"no body", ``, true, ""},
		{"only whitespace", "  \n", true, ""},
		{"an empty object", `{}`, true, ""},
		{"a known key", `{"name":"Ana"}`, true, "Ana"},
		{"an unknown key", `{"nmae":"Ana"}`, false, ""},
		{"not json", `{"name":`, false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			var into strictBody
			if got := DecodeOptionalStrictJSON(rec, req, &into); got != tc.ok {
				t.Fatalf("DecodeOptionalStrictJSON = %v, want %v (%s)", got, tc.ok, rec.Body.String())
			}
			if !tc.ok {
				if rec.Code != http.StatusBadRequest {
					t.Fatalf("status = %d, want 400", rec.Code)
				}
				return
			}
			if rec.Body.Len() != 0 {
				t.Fatalf("an accepted body must not write: %s", rec.Body.String())
			}
			got := ""
			if into.Name != nil {
				got = *into.Name
			}
			if got != tc.want {
				t.Fatalf("name = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDecodeOptionalStrictJSONRefusesAnOversizedBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"name":"`+strings.Repeat("a", maxOptionalBodyBytes)+`"}`))
	rec := httptest.NewRecorder()
	var into strictBody
	if DecodeOptionalStrictJSON(rec, req, &into) || rec.Code != http.StatusBadRequest {
		t.Fatalf("an oversized body must be refused, status = %d", rec.Code)
	}
}

func TestWriteVersionRequired(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteVersionRequired(rec)
	var out struct {
		Code string `json:"code"`
	}
	if rec.Code != http.StatusPreconditionRequired || json.Unmarshal(rec.Body.Bytes(), &out) != nil || out.Code != CodeVersionRequired {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
}
