package emailinbox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"

	domain "vozko/domain/emailinbox"
	"vozko/domain/emailsending"
	workspace_domain "vozko/domain/workspace"
	"vozko/infra/http/middleware"
	usecase "vozko/usecases/emailinbox"
	"vozko/usecases/shared/automationrefs"
)

const (
	testWorkspace = "ws-1"
	testSender    = "sender-1"
)

type stubInboxes struct {
	byID map[string]*domain.Inbox
}

func (s *stubInboxes) Ensure(_ context.Context, inbox domain.Inbox) (*domain.Inbox, error) {
	if existing, ok := s.byID[inbox.ID]; ok {
		return existing, nil
	}
	s.byID[inbox.ID] = &inbox
	return &inbox, nil
}

func (s *stubInboxes) FindByID(_ context.Context, workspaceID, id string) (*domain.Inbox, error) {
	if i, ok := s.byID[id]; ok && i.WorkspaceID == workspaceID {
		return i, nil
	}
	return nil, domain.ErrInboxNotFound
}

func (s *stubInboxes) FindByIDUnscoped(_ context.Context, id string) (*domain.Inbox, error) {
	return nil, domain.ErrInboxNotFound
}

func (s *stubInboxes) FindByIDsUnscoped(context.Context, []string) ([]*domain.Inbox, error) {
	return nil, nil
}

func (s *stubInboxes) ListByWorkspace(context.Context, string) ([]*domain.Inbox, error) {
	return nil, nil
}

func (s *stubInboxes) Update(_ context.Context, inbox *domain.Inbox) error {
	s.byID[inbox.ID] = inbox
	return nil
}

type stubSenders struct{}

func (stubSenders) Get(_ context.Context, workspaceID, id string) (*emailsending.Sender, error) {
	if workspaceID == testWorkspace && id == testSender {
		return &emailsending.Sender{ID: testSender, WorkspaceID: testWorkspace, DomainID: "d-1", LocalPart: "vendas", DisplayName: "Loja"}, nil
	}
	return nil, emailsending.ErrSenderNotFound
}

func (s stubSenders) ListByWorkspace(ctx context.Context, workspaceID string) ([]emailsending.Sender, error) {
	sender, err := s.Get(ctx, workspaceID, testSender)
	if err != nil {
		return nil, nil
	}
	return []emailsending.Sender{*sender}, nil
}

type stubDomains struct{}

func (stubDomains) Get(_ context.Context, workspaceID, id string) (*emailsending.SendingDomain, error) {
	return &emailsending.SendingDomain{ID: id, WorkspaceID: workspaceID, Domain: "loja.com.br"}, nil
}

type stubRefs struct{ err error }

func (s stubRefs) OwnedBy(context.Context, string, automationrefs.Refs) error { return s.err }

func router(refsErr error) *mux.Router {
	uc := usecase.NewInboxes(&stubInboxes{byID: map[string]*domain.Inbox{}}, stubSenders{}, stubDomains{}, stubRefs{err: refsErr})
	r := mux.NewRouter()
	pass := func(_ workspace_domain.Resource, _ workspace_domain.Action, next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, req *http.Request) {
			next(w, req.WithContext(context.WithValue(req.Context(), middleware.WorkspaceIDContextKey, testWorkspace)))
		}
	}
	RegisterProtectedRoutes(r, NewHandler(uc), pass)
	return r
}

func TestInboxRoutes(t *testing.T) {
	cases := []struct {
		name     string
		method   string
		path     string
		body     string
		refsErr  error
		status   int
		contains string
	}{
		{"list", http.MethodGet, "/email/inboxes", "", nil, http.StatusOK, `"address":"vendas@loja.com.br"`},
		{"get", http.MethodGet, "/email/inboxes/" + testSender, "", nil, http.StatusOK, `"id":"sender-1"`},
		{"get unknown", http.MethodGet, "/email/inboxes/other", "", nil, http.StatusNotFound, `inbox_not_found`},
		{"update", http.MethodPut, "/email/inboxes/" + testSender, `{"agentId":"a-1","enableAgentResponses":true}`, nil, http.StatusOK, `"agentId":"a-1"`},
		{"update with foreign reference", http.MethodPut, "/email/inboxes/" + testSender, `{"agentId":"a-9"}`, automationrefs.ErrNotInWorkspace, http.StatusUnprocessableEntity, `reference_not_in_workspace`},
		{"update with bad body", http.MethodPut, "/email/inboxes/" + testSender, `{`, nil, http.StatusBadRequest, `invalid_body`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			router(tc.refsErr).ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
			if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.contains) {
				t.Fatalf("%d %s", rec.Code, rec.Body.String())
			}
			if rec.Code == http.StatusOK && !json.Valid(rec.Body.Bytes()) {
				t.Fatal("invalid json")
			}
		})
	}
}
