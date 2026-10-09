package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"vozko/domain/aichat"
	"vozko/domain/auth"
	copilot_domain "vozko/domain/copilot"
	"vozko/infra/copilotscreen"
	"vozko/infra/http/middleware"
	aichat_usecase "vozko/usecases/aichat"
	"vozko/usecases/agentloop"
	copilot_usecase "vozko/usecases/copilot"
)

const screenCommandID = "0b9a5f9e-3c55-4c1e-9a43-6f0d2c7e8a11"

type screenThreads struct{ thread *aichat.Thread }

func (s screenThreads) Create(*aichat.Thread) error { return nil }
func (s screenThreads) GetByID(string) (*aichat.Thread, error) {
	if s.thread == nil {
		return nil, aichat.ErrThreadNotFound
	}
	return s.thread, nil
}
func (s screenThreads) ListByUser(aichat.ListThreadsInput) ([]*aichat.Thread, int64, error) {
	return nil, 0, nil
}
func (s screenThreads) Rename(string, string) error            { return nil }
func (s screenThreads) Touch(string, time.Time, string) error { return nil }
func (s screenThreads) Delete(string) error                    { return nil }

type loopback struct{ handler func([]byte) }

func (l *loopback) Publish(_ string, data []byte) error { l.handler(data); return nil }
func (l *loopback) Subscribe(_ context.Context, _ string, handler func([]byte)) {
	l.handler = handler
}

func screenHandler(owner string) (*AIChatHandler, *copilotscreen.Broker) {
	threads := screenThreads{thread: &aichat.Thread{ID: "th1", WorkspaceID: "ws1", UserID: owner}}
	broker := copilotscreen.NewBroker(&loopback{})
	broker.Start(context.Background())
	copilot := copilot_usecase.NewService(agentloop.Engine{}, copilot_usecase.NewRegistry(), nil, nil, threads, nil, nil, nil, nil)
	copilot.SetScreenMailbox(broker)
	return NewAIChatHandler(aichat_usecase.NewService(threads, nil, nil, nil), copilot), broker
}

func postScreenReply(h *AIChatHandler, userID, commandID, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "/chat/threads/th1/screen/"+commandID, strings.NewReader(body))
	r = mux.SetURLVars(r, map[string]string{"id": "th1", "commandId": commandID})
	ctx := context.WithValue(r.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: userID})
	ctx = context.WithValue(ctx, middleware.WorkspaceIDContextKey, "ws1")
	w := httptest.NewRecorder()
	h.ScreenReply(w, r.WithContext(ctx))
	return w
}

func TestTheEditorReplyWakesTheWaitingCommand(t *testing.T) {
	h, broker := screenHandler("u1")
	wait, cancel := broker.Expect(copilot_domain.ScreenKey("th1", screenCommandID))
	defer cancel()
	if w := postScreenReply(h, "u1", screenCommandID, `{"ok":true,"data":{"clips":3}}`); w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
	ctx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if reply, err := wait(ctx); err != nil || !reply.OK || string(reply.Data) != `{"clips":3}` {
		t.Fatalf("reply = %+v, %v", reply, err)
	}
}

func TestScreenRepliesFailClosed(t *testing.T) {
	cases := map[string]struct {
		user, command, body string
		status              int
	}{
		"someone else's thread": {"intruder", screenCommandID, `{"ok":true}`, http.StatusForbidden},
		"command that is not an id": {"u1", "../approve", `{"ok":true}`, http.StatusBadRequest},
		"malformed reply":           {"u1", screenCommandID, `{"ok":true,"eval":"x"}`, http.StatusBadRequest},
		"oversized reply":           {"u1", screenCommandID, `{"ok":true,"data":"` + strings.Repeat("a", copilot_domain.MaxScreenReplyBytes) + `"}`, http.StatusRequestEntityTooLarge},
	}
	for name, tc := range cases {
		h, broker := screenHandler("u1")
		wait, cancel := broker.Expect(copilot_domain.ScreenKey("th1", screenCommandID))
		if w := postScreenReply(h, tc.user, tc.command, tc.body); w.Code != tc.status {
			t.Fatalf("%s: status = %d, want %d", name, w.Code, tc.status)
		}
		ctx, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
		if _, err := wait(ctx); err == nil {
			t.Fatalf("%s: a refused reply must not reach the command", name)
		}
		stop()
		cancel()
	}
}

func TestAnUnknownExecutionModeIsRefused(t *testing.T) {
	h, _ := screenHandler("u1")
	for _, call := range []struct {
		path string
		run  func(http.ResponseWriter, *http.Request)
	}{
		{"/chat/threads/th1/messages", h.StreamMessage},
		{"/chat/threads/th1/actions/a1/approve", h.ApproveAction},
	} {
		r := httptest.NewRequest(http.MethodPost, call.path, strings.NewReader(`{"content":"oi","mode":"yolo"}`))
		r = mux.SetURLVars(r, map[string]string{"id": "th1", "actionId": "a1"})
		ctx := context.WithValue(r.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: "u1"})
		ctx = context.WithValue(ctx, middleware.WorkspaceIDContextKey, "ws1")
		w := httptest.NewRecorder()
		call.run(w, r.WithContext(ctx))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d", call.path, w.Code)
		}
	}
}
