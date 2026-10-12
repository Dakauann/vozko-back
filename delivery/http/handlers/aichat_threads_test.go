package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"vozko/domain/aichat"
	"vozko/usecases/agentloop"
	aichat_usecase "vozko/usecases/aichat"
	copilot_usecase "vozko/usecases/copilot"
)

const (
	boundProject = "11111111-1111-4111-8111-111111111111"
	openProject  = "22222222-2222-4222-8222-222222222222"
)

type listedThreads struct {
	screenThreads
	items []*aichat.Thread
	input aichat.ListThreadsInput
}

func (l *listedThreads) ListByUser(input aichat.ListThreadsInput) ([]*aichat.Thread, int64, error) {
	l.input = input
	return l.items, int64(len(l.items)), nil
}

type refusingProjects struct{ calls int }

func (r *refusingProjects) BindProject(string, string) (bool, error) {
	r.calls++
	return false, nil
}

func threadsHandler(threads aichat.ThreadRepository) (*AIChatHandler, *copilot_usecase.Service, *refusingProjects) {
	copilot := copilot_usecase.NewService(agentloop.Engine{}, copilot_usecase.NewRegistry(), nil, nil, threads, turnMessages{}, nil, nil, nil)
	chats := aichat_usecase.NewService(threads, turnMessages{}, nil, aichat_usecase.NewFundsGate(fundedBalance{}, activePlan{}))
	projects := &refusingProjects{}
	chats.SetThreadProjects(projects)
	return NewAIChatHandler(chats, copilot), copilot, projects
}

func TestTheThreadListShowsProjectsRunningAnswersAndSearches(t *testing.T) {
	at := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	threads := &listedThreads{items: []*aichat.Thread{
		{ID: "th1", WorkspaceID: "ws1", UserID: "u1", Title: "Banner", ProjectID: boundProject, CreatedAt: at},
		{ID: "th2", WorkspaceID: "ws1", UserID: "u1", Title: "Relatório", CreatedAt: at},
	}}
	h, copilot, _ := threadsHandler(threads)
	_, release := runningAnswer(t, copilot)
	defer close(release)

	w := call(h.ListThreads, turnRequest(context.Background(), http.MethodGet, "/chat/threads?q=%20ban%20&page=2&pageSize=10", "u1", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
	if threads.input.Query != "ban" || threads.input.WorkspaceID != "ws1" || threads.input.UserID != "u1" || threads.input.Limit != 10 || threads.input.Offset != 10 {
		t.Fatalf("the search must stay in the owner's workspace, got %+v", threads.input)
	}
	var body struct {
		Items []struct {
			ID        string `json:"id"`
			ProjectID string `json:"projectId"`
			Running   bool   `json:"running"`
		} `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || len(body.Items) != 2 {
		t.Fatalf("body = %s", w.Body.String())
	}
	if body.Items[0].ProjectID != boundProject || !body.Items[0].Running {
		t.Fatalf("the studio thread must show its project and its running answer, got %+v", body.Items[0])
	}
	if body.Items[1].ProjectID != "" || body.Items[1].Running {
		t.Fatalf("a plain idle thread must show neither, got %+v", body.Items[1])
	}
}

func TestTheHistoryTellsWhichProjectTheThreadBelongsTo(t *testing.T) {
	threads := screenThreads{thread: &aichat.Thread{ID: "th1", WorkspaceID: "ws1", UserID: "u1", ProjectID: boundProject}}
	h, _, _ := threadsHandler(threads)
	w := call(h.ListMessages, turnRequest(context.Background(), http.MethodGet, "/chat/threads/th1/messages", "u1", ""))
	var body struct {
		ProjectID string `json:"projectId"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &body) != nil || body.ProjectID != boundProject {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
}

func TestAStudioTurnOnAThreadOfAnotherProjectIsRefused(t *testing.T) {
	view := `"view":{"surface":"studio","projectId":"` + openProject + `","projectKind":"image"}`
	requests := map[string]struct {
		path string
		body string
	}{
		"message": {path: "/chat/threads/th1/messages", body: `{"content":"troca o fundo",` + view + `}`},
		"approve": {path: "/chat/threads/th1/actions/a1/approve", body: `{` + view + `}`},
		"reject":  {path: "/chat/threads/th1/actions/a1/reject", body: `{` + view + `}`},
	}
	for name, req := range requests {
		t.Run(name, func(t *testing.T) {
			threads := screenThreads{thread: &aichat.Thread{ID: "th1", WorkspaceID: "ws1", UserID: "u1", ProjectID: boundProject}}
			h, copilot, _ := threadsHandler(threads)
			run := map[string]func(http.ResponseWriter, *http.Request){"message": h.StreamMessage, "approve": h.ApproveAction, "reject": h.RejectAction}[name]
			w := call(run, turnRequest(context.Background(), http.MethodPost, req.path, "u1", req.body))
			if w.Code != http.StatusConflict || errorCode(t, w) != "thread_other_project" {
				t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
			}
			if copilot.TurnRunning("th1") {
				t.Fatal("a refused turn must not start")
			}
		})
	}
}

func TestAStudioTurnIsRefusedWhenTheBindingIsTakenMeanwhile(t *testing.T) {
	threads := screenThreads{thread: &aichat.Thread{ID: "th1", WorkspaceID: "ws1", UserID: "u1"}}
	h, copilot, projects := threadsHandler(threads)
	body := `{"content":"troca o fundo","view":{"surface":"studio","projectId":"` + openProject + `","projectKind":"video"}}`
	w := call(h.StreamMessage, turnRequest(context.Background(), http.MethodPost, "/chat/threads/th1/messages", "u1", body))
	if w.Code != http.StatusConflict || errorCode(t, w) != "thread_other_project" || projects.calls != 1 {
		t.Fatalf("status = %d, binds %d, body %s", w.Code, projects.calls, w.Body.String())
	}
	if copilot.TurnRunning("th1") {
		t.Fatal("a refused turn must not start")
	}
}
