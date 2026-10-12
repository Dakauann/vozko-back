package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"vozko/domain/aichat"
	"vozko/usecases/agentloop"
	aichat_usecase "vozko/usecases/aichat"
	copilot_usecase "vozko/usecases/copilot"
)

type memoryQueue struct {
	mu   sync.Mutex
	rows []*aichat.QueuedMessage
}

func (q *memoryQueue) Enqueue(m *aichat.QueuedMessage) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, row := range q.rows {
		if row.ClientID == m.ClientID {
			*m = *row
			return nil
		}
	}
	m.CreatedAt = time.Now()
	stored := *m
	q.rows = append(q.rows, &stored)
	return nil
}

func (q *memoryQueue) Pending(string) ([]*aichat.QueuedMessage, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]*aichat.QueuedMessage(nil), q.rows...), nil
}

func (q *memoryQueue) Remove(_, clientID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i, row := range q.rows {
		if row.ClientID == clientID {
			q.rows = append(q.rows[:i], q.rows[i+1:]...)
			return nil
		}
	}
	return aichat.ErrQueuedMessageGone
}

func (q *memoryQueue) Deliver(string) ([]*aichat.QueuedMessage, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := q.rows
	q.rows = nil
	return out, nil
}

func queueHandler() (*AIChatHandler, *copilot_usecase.Service, *memoryQueue) {
	threads := screenThreads{thread: &aichat.Thread{ID: "th1", WorkspaceID: "ws1", UserID: "u1"}}
	copilot := copilot_usecase.NewService(agentloop.Engine{}, copilot_usecase.NewRegistry(), nil, nil, threads, turnMessages{}, nil, nil, nil)
	queue := &memoryQueue{}
	copilot.SetQueue(queue)
	chats := aichat_usecase.NewService(threads, turnMessages{}, nil, aichat_usecase.NewFundsGate(fundedBalance{}, activePlan{}))
	return NewAIChatHandler(chats, copilot), copilot, queue
}

func queueRequest(method, path, userID, body, clientID string) *http.Request {
	r := turnRequest(context.Background(), method, path, userID, body)
	return mux.SetURLVars(r, map[string]string{"id": "th1", "clientId": clientID})
}

func TestAMessageSentWhileElOWorksIsQueuedNotAnswered(t *testing.T) {
	h, copilot, queue := queueHandler()
	_, release := runningAnswer(t, copilot)
	defer close(release)
	w := call(h.QueueMessage, queueRequest(http.MethodPost, "/chat/threads/th1/queue", "u1", `{"clientId":"c1","content":"e o azul?"}`, ""))
	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
	var view copilot_usecase.QueuedView
	if err := json.Unmarshal(w.Body.Bytes(), &view); err != nil || view.ClientID != "c1" || view.Content != "e o azul?" {
		t.Fatalf("body = %s", w.Body.String())
	}
	if len(queue.rows) != 1 {
		t.Fatalf("rows = %d", len(queue.rows))
	}
	list := call(h.ListMessages, turnRequest(context.Background(), http.MethodGet, "/chat/threads/th1/messages", "u1", ""))
	if !strings.Contains(list.Body.String(), `"queued":[{"clientId":"c1"`) {
		t.Fatalf("the history must carry the queue, got %s", list.Body.String())
	}
}

func TestQueueingOnAnIdleThreadTellsTheClientToSendNormally(t *testing.T) {
	h, _, queue := queueHandler()
	w := call(h.QueueMessage, queueRequest(http.MethodPost, "/chat/threads/th1/queue", "u1", `{"clientId":"c1","content":"oi"}`, ""))
	if w.Code != http.StatusNotFound || errorCode(t, w) != "turn_not_running" || len(queue.rows) != 0 {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
}

func TestQueueingRejectsABadClientIDAndSomeoneElsesThread(t *testing.T) {
	h, copilot, _ := queueHandler()
	_, release := runningAnswer(t, copilot)
	defer close(release)
	if w := call(h.QueueMessage, queueRequest(http.MethodPost, "/chat/threads/th1/queue", "u1", `{"clientId":"a b","content":"oi"}`, "")); w.Code != http.StatusBadRequest {
		t.Fatalf("bad id: status = %d", w.Code)
	}
	if w := call(h.QueueMessage, queueRequest(http.MethodPost, "/chat/threads/th1/queue", "intruder", `{"clientId":"c1","content":"oi"}`, "")); w.Code != http.StatusForbidden {
		t.Fatalf("intruder: status = %d", w.Code)
	}
}

func TestRemovingADeliveredMessageSaysItIsGone(t *testing.T) {
	h, copilot, queue := queueHandler()
	_, release := runningAnswer(t, copilot)
	defer close(release)
	call(h.QueueMessage, queueRequest(http.MethodPost, "/chat/threads/th1/queue", "u1", `{"clientId":"c1","content":"oi"}`, ""))
	if w := call(h.UnqueueMessage, queueRequest(http.MethodDelete, "/chat/threads/th1/queue/c1", "u1", "", "c1")); w.Code != http.StatusOK || len(queue.rows) != 0 {
		t.Fatalf("remove: status = %d, body %s", w.Code, w.Body.String())
	}
	if w := call(h.UnqueueMessage, queueRequest(http.MethodDelete, "/chat/threads/th1/queue/c1", "u1", "", "c1")); w.Code != http.StatusNotFound || errorCode(t, w) != "queued_message_gone" {
		t.Fatalf("second remove: status = %d, body %s", w.Code, w.Body.String())
	}
}
