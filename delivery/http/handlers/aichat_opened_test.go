package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"vozko/domain/aichat"
	"vozko/usecases/agentloop"
	aichat_usecase "vozko/usecases/aichat"
	copilot_usecase "vozko/usecases/copilot"
)

const openedProject = "6a1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d"

func openedHandler(owner, bound string) (*AIChatHandler, *copilot_usecase.Service) {
	threads := screenThreads{thread: &aichat.Thread{ID: "th1", WorkspaceID: "ws1", UserID: owner, ProjectID: bound}}
	copilot := copilot_usecase.NewService(agentloop.Engine{}, copilot_usecase.NewRegistry(), nil, nil, threads, turnMessages{}, nil, nil, nil)
	return NewAIChatHandler(aichat_usecase.NewService(threads, turnMessages{}, nil, nil), copilot), copilot
}

func openedBody(project string) string {
	return `{"view":{"surface":"studio","projectId":"` + project + `","projectKind":"image"},"mode":"edit"}`
}

func outcomeOf(t *testing.T, w interface{ Bytes() []byte }) string {
	t.Helper()
	var body struct {
		Outcome string `json:"outcome"`
	}
	if err := json.Unmarshal(w.Bytes(), &body); err != nil {
		t.Fatalf("body %s: %v", w.Bytes(), err)
	}
	return body.Outcome
}

func TestAnOpenedEditorReachesTheRunningAnswer(t *testing.T) {
	h, copilot := openedHandler("u1", openedProject)
	_, release := runningAnswer(t, copilot)
	defer close(release)
	w := call(h.ProjectOpened, turnRequest(context.Background(), http.MethodPost, "/chat/threads/th1/project-opened", "u1", openedBody(openedProject)))
	if w.Code != http.StatusAccepted || outcomeOf(t, w.Body) != string(copilot_usecase.OpenDelivered) {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
}

func TestAnOpenedEditorOfAnotherProjectIsIgnored(t *testing.T) {
	h, _ := openedHandler("u1", openedProject)
	other := "7b2c3d4e-5f6a-4b7c-8d9e-0f1a2b3c4d5e"
	w := call(h.ProjectOpened, turnRequest(context.Background(), http.MethodPost, "/chat/threads/th1/project-opened", "u1", openedBody(other)))
	if w.Code != http.StatusAccepted || outcomeOf(t, w.Body) != string(copilot_usecase.OpenIgnored) {
		t.Fatalf("status %d body %s", w.Code, w.Body.String())
	}
}

func TestAnOpenedEditorNeedsAStudioScreenAndTheOwner(t *testing.T) {
	h, _ := openedHandler("u1", openedProject)
	for name, body := range map[string]string{
		"no studio":    `{"view":{}}`,
		"broken view":  `{"view":{"surface":"studio","projectId":"p1","projectKind":"image"}}`,
		"unknown mode": `{"view":{"surface":"studio","projectId":"` + openedProject + `","projectKind":"image"},"mode":"turbo"}`,
		"not json":     `aberto`,
	} {
		if w := call(h.ProjectOpened, turnRequest(context.Background(), http.MethodPost, "/chat/threads/th1/project-opened", "u1", body)); w.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d", name, w.Code)
		}
	}
	if w := call(h.ProjectOpened, turnRequest(context.Background(), http.MethodPost, "/chat/threads/th1/project-opened", "intruder", openedBody(openedProject))); w.Code != http.StatusForbidden && w.Code != http.StatusNotFound {
		t.Fatalf("someone else's thread: status %d", w.Code)
	}
}
