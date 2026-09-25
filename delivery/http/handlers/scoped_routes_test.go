package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	"vozko/domain/rag"
	wc "vozko/domain/whatsapp_campaign"
	dept "vozko/domain/workspace/workspace_department"
	"vozko/infra/http/middleware"
	rag_usecase "vozko/usecases/rag"
)

func scopedRequest(method, body string, vars map[string]string, departments ...string) *http.Request {
	req := httptest.NewRequest(method, "/", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), middleware.WorkspaceIDContextKey, "ws1")
	ctx = context.WithValue(ctx, middleware.DepartmentFilterContextKey, &dept.DepartmentFilter{DepartmentIDs: departments, WorkspaceHasDepartments: true})
	return mux.SetURLVars(req.WithContext(ctx), vars)
}

type startStub struct {
	err     error
	started bool
}

func (s *startStub) Start(workspaceID string, _ *dept.DepartmentFilter, id string) (*wc.Campaign, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.started = workspaceID == "ws1" && id == "c1"
	return &wc.Campaign{ID: id}, nil
}

func TestStartCampaignRouteMapsEveryRefusal(t *testing.T) {
	cases := []struct {
		err  error
		want int
	}{
		{nil, http.StatusAccepted},
		{wc.ErrCampaignNotFound, http.StatusNotFound},
		{wc.ErrCampaignNoSubscription, http.StatusPaymentRequired},
		{wc.ErrCampaignNoNumbers, http.StatusBadRequest},
		{wc.ErrCampaignAllProcessed, http.StatusBadRequest},
		{wc.ErrDispatchCampaignAlreadyRunning, http.StatusBadRequest},
		{errors.New("queue down"), http.StatusInternalServerError},
	}
	for _, c := range cases {
		h := &WhatsAppCampaignHandler{}
		start := &startStub{err: c.err}
		h.SetStartCampaign(start)
		rec := httptest.NewRecorder()
		h.StartCampaign(rec, scopedRequest(http.MethodPost, "", map[string]string{"id": "c1"}, "d-sales"))
		if rec.Code != c.want {
			t.Fatalf("%v: status %d, want %d (%s)", c.err, rec.Code, c.want, rec.Body.String())
		}
		if c.err == nil && !start.started {
			t.Fatal("campaign was not started with the request workspace")
		}
	}
}

func TestStartCampaignRouteFailsClosedWithoutTheUseCase(t *testing.T) {
	rec := httptest.NewRecorder()
	(&WhatsAppCampaignHandler{}).StartCampaign(rec, scopedRequest(http.MethodPost, "", map[string]string{"id": "c1"}, "d-sales"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d", rec.Code)
	}
}

type kbStub map[string]*rag.KnowledgeBase

func (k kbStub) Execute(_ context.Context, id string) (*rag.KnowledgeBase, error) {
	if kb, ok := k[id]; ok {
		return kb, nil
	}
	return nil, rag.ErrKnowledgeBaseNotFound
}

type docStub struct{ created []rag.CreateDocumentInput }

func (d *docStub) Execute(_ context.Context, in rag.CreateDocumentInput) (*rag.Document, error) {
	d.created = append(d.created, in)
	return &rag.Document{ID: "doc1", KnowledgeBaseID: in.KnowledgeBaseID}, nil
}

func knowledgeHandler(docs *docStub) *KnowledgeBaseHandler {
	access := rag_usecase.NewKnowledgeBaseAccessUseCase(kbStub{
		"kb-sales":   {ID: "kb-sales", WorkspaceID: "ws1", DepartmentID: "d-sales"},
		"kb-support": {ID: "kb-support", WorkspaceID: "ws1", DepartmentID: "d-support"},
		"kb-foreign": {ID: "kb-foreign", WorkspaceID: "ws2"},
	})
	return NewKnowledgeBaseHandler(nil, nil, nil, access, nil, rag_usecase.NewScopedDocumentsUseCase(access, docs), nil, nil, nil, nil, nil, nil)
}

func TestKnowledgeBaseRoutesHideOtherDepartmentsAndWorkspaces(t *testing.T) {
	h := knowledgeHandler(&docStub{})
	for id, want := range map[string]int{"kb-sales": http.StatusOK, "kb-support": http.StatusForbidden, "kb-foreign": http.StatusForbidden, "nope": http.StatusNotFound} {
		rec := httptest.NewRecorder()
		h.Get(rec, scopedRequest(http.MethodGet, "", map[string]string{"id": id}, "d-sales"))
		if rec.Code != want {
			t.Fatalf("%s: status %d, want %d", id, rec.Code, want)
		}
	}
}

func TestKnowledgeDocumentRouteOnlyWritesToAVisibleBase(t *testing.T) {
	docs := &docStub{}
	h := knowledgeHandler(docs)
	body := `{"name":"faq","content":"texto"}`
	rec := httptest.NewRecorder()
	h.CreateDocument(rec, scopedRequest(http.MethodPost, body, map[string]string{"id": "kb-support"}, "d-sales"))
	if rec.Code != http.StatusForbidden || len(docs.created) != 0 {
		t.Fatalf("other department: status %d created %v", rec.Code, docs.created)
	}
	rec = httptest.NewRecorder()
	h.CreateDocument(rec, scopedRequest(http.MethodPost, body, map[string]string{"id": "kb-sales"}, "d-sales"))
	if rec.Code != http.StatusCreated || len(docs.created) != 1 {
		t.Fatalf("own base: status %d created %v", rec.Code, docs.created)
	}
}
