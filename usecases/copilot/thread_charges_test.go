package copilot_usecase

import (
	"context"
	"testing"

	"vozko/domain/ai"
	"vozko/domain/aichat"
	"vozko/domain/copilot"
)

func TestEveryModelCallOfAnAnswerIsChargedToItsThread(t *testing.T) {
	th := &fakeThreads{thread: &aichat.Thread{ID: "th1", WorkspaceID: "ws1", UserID: "u1"}}
	tool := &fakeTool{name: "list_agents", meta: readMeta}
	prov := &scriptAI{turns: [][]ai.ToolCall{{call("list_agents", nil)}, {}}, texts: []string{"", "ok"}}
	svc := newService(prov, th, &fakeMessages{}, tool)
	if err := svc.Stream(context.Background(), th.thread, copilot.UserMessage{Content: "liste"}, ownerCtx, (&capture{}).emit); err != nil {
		t.Fatal(err)
	}
	if len(prov.inputs) == 0 {
		t.Fatal("the answer made no model call")
	}
	for i, in := range prov.inputs {
		if in.BillingReference != aichat.ChargeReference("th1") {
			t.Fatalf("model call %d must be charged to the thread, got %q", i, in.BillingReference)
		}
	}
	if tool.gotCC.ChargeReference != aichat.ChargeReference("th1") {
		t.Fatalf("tools must know the thread to charge, got %q", tool.gotCC.ChargeReference)
	}
}

func TestAnApprovedActionIsChargedToItsThread(t *testing.T) {
	th := &fakeThreads{thread: testThread()}
	ms := &fakeMessages{}
	wt := &fakeTool{name: "create_agent", meta: writeMeta}
	prov := &scriptAI{turns: [][]ai.ToolCall{{}}, texts: []string{"Pronto."}}
	svc := newService(prov, th, ms, wt)
	ms.propose(copilot.PendingAction{ID: "act-1", ToolName: "create_agent", Args: map[string]interface{}{"name": "Bot"}})
	if err := svc.Approve(context.Background(), th.thread, "act-1", copilot.Approval{}, ownerCtx, (&capture{}).emit); err != nil {
		t.Fatal(err)
	}
	if wt.gotCC.ChargeReference != aichat.ChargeReference(th.thread.ID) {
		t.Fatalf("the approved tool must know the thread to charge, got %q", wt.gotCC.ChargeReference)
	}
}
