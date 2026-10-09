package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"

	"vozko/domain/aichat"
	"vozko/domain/aiusage"
	"vozko/domain/auth"
	"vozko/domain/balance"
	"vozko/domain/workspace/workspace_plan"
	"vozko/infra/http/middleware"
	"vozko/usecases/agentloop"
	aichat_usecase "vozko/usecases/aichat"
	copilot_usecase "vozko/usecases/copilot"
)

type turnMessages struct{}

func (turnMessages) Create(*aichat.Message) error { return nil }
func (turnMessages) ListByThread(aichat.ListMessagesInput) ([]*aichat.Message, int64, error) {
	return []*aichat.Message{{ID: "m1", ThreadID: "th1", Role: aichat.RoleUser, Content: "oi"}}, 1, nil
}
func (turnMessages) DeleteByThread(string) error { return nil }
func (turnMessages) ClaimProposal(string, string, aichat.ProposalStatus) (*aichat.Message, error) {
	return nil, nil
}
func (turnMessages) ExpireProposals(string) error { return nil }

type fundedBalance struct{}

func (fundedBalance) HasSufficientBalance(string, int64) (bool, error) { return true, nil }
func (fundedBalance) GetBalance(string) (int64, error)                 { return 1 << 40, nil }
func (fundedBalance) Invalidate(string)                                {}
func (fundedBalance) InvalidateDebounced(string)                       {}

type activePlan struct{}

func (activePlan) GetCurrentByWorkspaceID(string, time.Time) (*workspace_plan.WorkspaceSubscription, error) {
	return &workspace_plan.WorkspaceSubscription{}, nil
}

func turnHandler(owner string) (*AIChatHandler, *copilot_usecase.Service) {
	threads := screenThreads{thread: &aichat.Thread{ID: "th1", WorkspaceID: "ws1", UserID: owner}}
	copilot := copilot_usecase.NewService(agentloop.Engine{}, copilot_usecase.NewRegistry(), nil, nil, threads, nil, nil, nil, nil)
	chats := aichat_usecase.NewService(threads, turnMessages{}, nil, aichat_usecase.NewFundsGate(fundedBalance{}, activePlan{}))
	return NewAIChatHandler(chats, copilot), copilot
}

func turnRequest(ctx context.Context, method, path, userID, body string) *http.Request {
	r := httptest.NewRequestWithContext(ctx, method, path, strings.NewReader(body))
	r = mux.SetURLVars(r, map[string]string{"id": "th1", "actionId": "a1"})
	scoped := context.WithValue(r.Context(), middleware.ClaimsContextKey, &auth.Claims{UserID: userID})
	scoped = context.WithValue(scoped, middleware.WorkspaceIDContextKey, "ws1")
	return r.WithContext(scoped)
}

func call(run func(http.ResponseWriter, *http.Request), r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	run(w, r)
	return w
}

func errorCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	return body.Code
}

func runningAnswer(t *testing.T, copilot *copilot_usecase.Service) (*copilot_usecase.Turn, chan struct{}) {
	t.Helper()
	turn, err := copilot.BeginTurn(context.Background(), "th1", "u1")
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	turn.Run(func(ctx context.Context, emit agentloop.Emit) error {
		emit("assistant_delta", map[string]string{"text": "Trabalhando"})
		select {
		case <-release:
			emit("done", map[string]interface{}{"content": "Trabalhando"})
		case <-ctx.Done():
		}
		return nil
	})
	return turn, release
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestWithNoRunningAnswerObserveAndStopSaySo(t *testing.T) {
	h, _ := turnHandler("u1")
	if w := call(h.ObserveTurn, turnRequest(context.Background(), http.MethodGet, "/chat/threads/th1/turn/events", "u1", "")); w.Code != http.StatusNotFound || errorCode(t, w) != "turn_not_running" {
		t.Fatalf("observe: status = %d, body %s", w.Code, w.Body.String())
	}
	if w := call(h.StopTurn, turnRequest(context.Background(), http.MethodPost, "/chat/threads/th1/turn/stop", "u1", "")); w.Code != http.StatusNotFound || errorCode(t, w) != "turn_not_running" {
		t.Fatalf("stop: status = %d, body %s", w.Code, w.Body.String())
	}
}

func TestSomeoneElsesAnswerCannotBeObservedOrStopped(t *testing.T) {
	h, copilot := turnHandler("u1")
	turn, _ := runningAnswer(t, copilot)
	if w := call(h.ObserveTurn, turnRequest(context.Background(), http.MethodGet, "/chat/threads/th1/turn/events", "intruder", "")); w.Code != http.StatusForbidden {
		t.Fatalf("observe: status = %d", w.Code)
	}
	if w := call(h.StopTurn, turnRequest(context.Background(), http.MethodPost, "/chat/threads/th1/turn/stop", "intruder", "")); w.Code != http.StatusForbidden {
		t.Fatalf("stop: status = %d", w.Code)
	}
	if turn.Context().Err() != nil {
		t.Fatal("a refused stop must leave the answer running")
	}
}

func TestTheOwnerStopsTheRunningAnswer(t *testing.T) {
	h, copilot := turnHandler("u1")
	turn, _ := runningAnswer(t, copilot)
	if w := call(h.StopTurn, turnRequest(context.Background(), http.MethodPost, "/chat/threads/th1/turn/stop", "u1", "")); w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
	if turn.Context().Err() == nil {
		t.Fatal("the answer must end")
	}
}

func TestObservingReplaysTheAnswerAndFollowsItToTheEnd(t *testing.T) {
	h, copilot := turnHandler("u1")
	_, release := runningAnswer(t, copilot)
	waitFor(t, "the first delta", func() bool {
		turn, _ := copilot.ObserveTurn("th1", "u1")
		replay, _, leave := turn.Subscribe()
		leave()
		return len(replay) == 1
	})
	w := httptest.NewRecorder()
	finished := make(chan struct{})
	go func() {
		h.ObserveTurn(w, turnRequest(context.Background(), http.MethodGet, "/chat/threads/th1/turn/events", "u1", ""))
		close(finished)
	}()
	waitFor(t, "the observer to attach", func() bool {
		turn, _ := copilot.ObserveTurn("th1", "u1")
		return turn != nil
	})
	time.Sleep(20 * time.Millisecond)
	close(release)
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("the observer must end with the answer")
	}
	body := w.Body.String()
	if w.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("content type = %q", w.Header().Get("Content-Type"))
	}
	delta := strings.Index(body, `"type":"assistant_delta"`)
	done := strings.Index(body, `"type":"done"`)
	if delta < 0 || done < 0 || delta > done || !strings.Contains(body, `"text":"Trabalhando"`) {
		t.Fatalf("the observer must replay then follow, got %s", body)
	}
}

func TestAMessageWhileAnAnswerRunsIsRefused(t *testing.T) {
	h, copilot := turnHandler("u1")
	runningAnswer(t, copilot)
	for _, c := range []struct {
		path string
		run  func(http.ResponseWriter, *http.Request)
	}{
		{"/chat/threads/th1/messages", h.StreamMessage},
		{"/chat/threads/th1/actions/a1/approve", h.ApproveAction},
	} {
		w := call(c.run, turnRequest(context.Background(), http.MethodPost, c.path, "u1", `{"content":"de novo"}`))
		if w.Code != http.StatusConflict || errorCode(t, w) != "turn_running" {
			t.Fatalf("%s: status = %d, body %s", c.path, w.Code, w.Body.String())
		}
	}
}

func TestTheHistorySaysWhetherAnAnswerIsRunning(t *testing.T) {
	h, copilot := turnHandler("u1")
	running := func() bool {
		w := call(h.ListMessages, turnRequest(context.Background(), http.MethodGet, "/chat/threads/th1/messages", "u1", ""))
		var body struct {
			Running *bool `json:"running"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Running == nil {
			t.Fatalf("the history must carry running, got %s", w.Body.String())
		}
		return *body.Running
	}
	if running() {
		t.Fatal("no answer is running yet")
	}
	_, release := runningAnswer(t, copilot)
	if !running() {
		t.Fatal("the running answer must show in the history")
	}
	close(release)
	waitFor(t, "the answer to end", func() bool { return !copilot.TurnRunning("th1") })
	if running() {
		t.Fatal("a finished answer is not running")
	}
}

func TestADisconnectedStreamLeavesTheAnswerRunning(t *testing.T) {
	h, copilot := turnHandler("u1")
	turn, release := runningAnswer(t, copilot)
	defer close(release)
	ctx, disconnect := context.WithCancel(context.Background())
	finished := make(chan struct{})
	go func() {
		h.ObserveTurn(httptest.NewRecorder(), turnRequest(ctx, http.MethodGet, "/chat/threads/th1/turn/events", "u1", ""))
		close(finished)
	}()
	time.Sleep(20 * time.Millisecond)
	disconnect()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("the stream must end when its client leaves")
	}
	if turn.Context().Err() != nil {
		t.Fatal("a client leaving must not end the answer at once")
	}
}

type pricedUsage struct{ totals aiusage.Totals }

func (p pricedUsage) TotalsUnder(string, string) (aiusage.Totals, error) {
	return p.totals, nil
}

type pricedLedger struct{ totals balance.ReferenceTotals }

func (p pricedLedger) TotalsUnder(string, string) (balance.ReferenceTotals, error) {
	return p.totals, nil
}

func costHandler(owner string, tracked bool) *AIChatHandler {
	threads := screenThreads{thread: &aichat.Thread{ID: "th1", WorkspaceID: "ws1", UserID: owner, CostTracked: tracked}}
	chats := aichat_usecase.NewService(threads, turnMessages{}, nil, nil)
	chats.SetChargeLedger(pricedLedger{totals: balance.ReferenceTotals{LedgerMicros: 80_000, BillingMicros: 420_000, Transactions: 2, Debits: 2}})
	chats.SetUsageLedger(pricedUsage{totals: aiusage.Totals{Calls: 2, Billed: 2, Tokens: aiusage.Tokens{Input: 90_000, Output: 1_200, CacheRead: 80_000, CacheWrite: 9_000, Reasoning: 300}}})
	copilot := copilot_usecase.NewService(agentloop.Engine{}, copilot_usecase.NewRegistry(), nil, nil, threads, nil, nil, nil, nil)
	return NewAIChatHandler(chats, copilot)
}

func TestTheThreadCostIsWhatWasCharged(t *testing.T) {
	w := call(costHandler("u1", true).ThreadCost, turnRequest(context.Background(), http.MethodGet, "/chat/threads/th1/cost", "u1", ""))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
	var body struct {
		Available    bool   `json:"available"`
		AmountMicros int64  `json:"amountMicros"`
		Currency     string `json:"currency"`
		Usage        struct {
			Available        bool  `json:"available"`
			Calls            int   `json:"calls"`
			InputTokens      int64 `json:"inputTokens"`
			OutputTokens     int64 `json:"outputTokens"`
			CacheReadTokens  int64 `json:"cacheReadTokens"`
			CacheWriteTokens int64 `json:"cacheWriteTokens"`
			ReasoningTokens  int64 `json:"reasoningTokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || !body.Available || body.AmountMicros != 420_000 || body.Currency != "BRL" {
		t.Fatalf("cost = %s", w.Body.String())
	}
	if u := body.Usage; !u.Available || u.Calls != 2 || u.InputTokens != 90_000 || u.OutputTokens != 1_200 || u.CacheReadTokens != 80_000 || u.CacheWriteTokens != 9_000 || u.ReasoningTokens != 300 {
		t.Fatalf("usage = %s", w.Body.String())
	}
	w = call(costHandler("u1", false).ThreadCost, turnRequest(context.Background(), http.MethodGet, "/chat/threads/th1/cost", "u1", ""))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"available":false`) {
		t.Fatalf("an untracked thread must say the cost is unavailable, got %d %s", w.Code, w.Body.String())
	}
}

func TestSomeoneElsesThreadCostIsNotFound(t *testing.T) {
	w := call(costHandler("u1", true).ThreadCost, turnRequest(context.Background(), http.MethodGet, "/chat/threads/th1/cost", "intruder", ""))
	if w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), "420000") {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
}
