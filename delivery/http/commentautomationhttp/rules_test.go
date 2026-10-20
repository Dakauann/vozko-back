package commentautomationhttp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vozko/delivery/http/response"
	ca "vozko/domain/commentautomation"
	"vozko/domain/shared"
)

type fakeManager struct {
	got     *ca.Rule
	deleted string
	err     error
	rules   []*ca.Rule
}

func (f *fakeManager) List(_ context.Context, ws string, source shared.EntryType, accountID string) ([]*ca.Rule, error) {
	f.got = &ca.Rule{WorkspaceID: ws, Source: source, AccountID: accountID}
	return f.rules, f.err
}

func (f *fakeManager) Create(_ context.Context, rule *ca.Rule) (*ca.Rule, error) {
	f.got = rule
	if f.err != nil {
		return nil, f.err
	}
	rule.ID = "rule-1"
	return rule, nil
}

func (f *fakeManager) Update(_ context.Context, rule *ca.Rule) (*ca.Rule, error) {
	f.got = rule
	return rule, f.err
}

func (f *fakeManager) Delete(_ context.Context, ws string, source shared.EntryType, accountID, id string) error {
	f.got = &ca.Rule{WorkspaceID: ws, Source: source, AccountID: accountID}
	f.deleted = id
	return f.err
}

func otherErrors(w http.ResponseWriter, err error, fallback string) {
	response.WriteError(w, http.StatusTeapot, fallback, nil)
}

func TestRulesAreBoundToTheSourceAndTheAccountInTheURL(t *testing.T) {
	m := &fakeManager{}
	h := NewRulesHandler(shared.EntryTypeMercadoLivre, m, otherErrors)
	body := `{"name":"Cor","enabled":true,"containerId":"MLB1","match":"contains","keywords":["azul"],"actions":["public_reply"],"publicReplyText":"Temos","priority":2}`
	rec := httptest.NewRecorder()
	h.Create(rec, httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body)), "ws-1", "acc-1")
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d %s", rec.Code, rec.Body)
	}
	if m.got.Source != shared.EntryTypeMercadoLivre || m.got.AccountID != "acc-1" || m.got.WorkspaceID != "ws-1" || m.got.ContainerID != "MLB1" ||
		len(m.got.Actions) != 1 || m.got.Actions[0] != ca.ActionPublicReply || m.got.Priority != 2 {
		t.Fatalf("rule = %+v", m.got)
	}
	var out RuleResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.ID != "rule-1" || out.AccountID != "acc-1" || out.Source != "mercadolivre" || out.ContainerID != "MLB1" {
		t.Fatalf("response = %s", rec.Body)
	}

	rec = httptest.NewRecorder()
	h.Update(rec, httptest.NewRequest(http.MethodPatch, "/x", strings.NewReader(body)), "ws-1", "acc-1", "rule-9")
	if rec.Code != http.StatusOK || m.got.ID != "rule-9" || m.got.AccountID != "acc-1" {
		t.Fatalf("update = %d %+v", rec.Code, m.got)
	}

	rec = httptest.NewRecorder()
	h.Delete(rec, httptest.NewRequest(http.MethodDelete, "/x", nil), "ws-1", "acc-1", "rule-9")
	if rec.Code != http.StatusOK || m.deleted != "rule-9" || m.got.Source != shared.EntryTypeMercadoLivre {
		t.Fatalf("delete = %d %+v", rec.Code, m.got)
	}

	m.rules = []*ca.Rule{{ID: "r", Source: shared.EntryTypeMercadoLivre}}
	rec = httptest.NewRecorder()
	h.List(rec, httptest.NewRequest(http.MethodGet, "/x", nil), "ws-1", "acc-1")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"keywords":[]`) || m.got.AccountID != "acc-1" {
		t.Fatalf("list = %d %s", rec.Code, rec.Body)
	}
}

func TestRuleErrorsMapToTheSharedCodes(t *testing.T) {
	cases := []struct {
		err  error
		code int
	}{
		{ca.ErrActionUnsupported, http.StatusBadRequest},
		{ca.ErrReplyTooLong, http.StatusBadRequest},
		{ca.ErrRuleNotFound, http.StatusNotFound},
		{errors.New("account gone"), http.StatusTeapot},
	}
	for _, tc := range cases {
		h := NewRulesHandler(shared.EntryTypeMercadoLivre, &fakeManager{err: tc.err}, otherErrors)
		rec := httptest.NewRecorder()
		h.Create(rec, httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"name":"x"}`)), "ws-1", "acc-1")
		if rec.Code != tc.code {
			t.Errorf("%v: status = %d", tc.err, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	NewRulesHandler(shared.EntryTypeMercadoLivre, &fakeManager{}, otherErrors).Create(rec, httptest.NewRequest(http.MethodPost, "/x", strings.NewReader("{")), "ws-1", "acc-1")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("a broken body = %d", rec.Code)
	}
}

func TestTheRulesHandlerNeedsItsDependencies(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("a handler without a manager must not be built")
		}
	}()
	NewRulesHandler(shared.EntryTypeMercadoLivre, nil, otherErrors)
}
