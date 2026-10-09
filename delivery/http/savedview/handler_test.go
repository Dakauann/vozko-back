package savedview

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/auth"
	"vozko/domain/crmfilter"
	"vozko/domain/customfield"
	"vozko/domain/lead"
	savedviewdomain "vozko/domain/savedview"
	"vozko/infra/http/middleware"
)

type createStub struct {
	err   error
	asked savedviewdomain.Actor
}

func (s *createStub) Execute(a savedviewdomain.Actor, v *savedviewdomain.SavedView) (*savedviewdomain.SavedView, error) {
	s.asked = a
	if s.err != nil {
		return nil, s.err
	}
	return v, nil
}

func createRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/saved-views", bytes.NewBufferString(body))
	ctx := context.WithValue(req.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: "user-1", Role: "member"})
	ctx = context.WithValue(ctx, middleware.WorkspaceIDContextKey, "ws-1")
	return req.WithContext(ctx)
}

func TestCreateIsAskedAsTheCallerAndKeepsTheColumns(t *testing.T) {
	stub := &createStub{}
	h := NewSavedViewHandler(stub, nil, nil, nil, nil)
	router := mux.NewRouter()
	RegisterRoutes(router, h)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, createRequest(`{"name":"Centro","objectType":"lead","columns":["name","district"]}`))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d (%s)", rec.Code, rec.Body.String())
	}
	if stub.asked.UserID != "user-1" || stub.asked.WorkspaceID != "ws-1" {
		t.Fatalf("actor = %+v", stub.asked)
	}
	var out savedviewdomain.SavedView
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || len(out.Columns) != 2 {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestSavedViewRefusalsCarryTheirStatusAndCode(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"no permission for the object", savedviewdomain.ErrForbidden, http.StatusForbidden, CodeForbidden},
		{"a sensitive filter", &customfield.FilterError{Key: "classificacao", Err: customfield.ErrFilterSensitive}, http.StatusForbidden, "custom_field_filter_sensitive_forbidden"},
		{"a lead filter that cannot match", errors.Join(lead.ErrLeadFilterInvalid, crmfilter.ErrInvalidValue), http.StatusBadRequest, "lead_filter_invalid"},
		{"an unknown custom key", &customfield.FilterError{Key: "x", Err: customfield.ErrFilterUnknownKey}, http.StatusBadRequest, "custom_field_filter_unknown_key"},
		{"someone else's view", savedviewdomain.ErrUnauthorized, http.StatusForbidden, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := NewSavedViewHandler(&createStub{err: tc.err}, nil, nil, nil, nil)
			rec := httptest.NewRecorder()
			h.Create(rec, createRequest(`{"name":"Centro","objectType":"lead"}`))
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tc.status, rec.Body.String())
			}
			if tc.code == "" {
				return
			}
			var body struct {
				Code string `json:"code"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Code != tc.code {
				t.Fatalf("code = %q, want %q", body.Code, tc.code)
			}
		})
	}
}
