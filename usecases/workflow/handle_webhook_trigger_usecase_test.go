package workflow_usecase

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"vozko/domain/workflow"
)

func headerFunc(h map[string]string) func(string) string {
	return func(name string) string { return h[name] }
}

type webhookHarness struct {
	uc       HandleWebhookTriggerUseCase
	webhooks *fakeWebhookRepo
	wfRepo   *MockWorkflowRepository
	runRepo  *MockWorkflowRunRepository
	entries  *fakeEntryChecker
	resolver *fakeEntryResolver
	launcher *fakeLauncher
	dedup    *fakeDedup
	state    *fakeSharedState
}

func newWebhookHarness() *webhookHarness {
	h := &webhookHarness{
		webhooks: newFakeWebhookRepo(),
		wfRepo:   NewMockWorkflowRepository(),
		runRepo:  NewMockWorkflowRunRepository(),
		entries:  &fakeEntryChecker{owns: true},
		resolver: &fakeEntryResolver{},
		launcher: &fakeLauncher{},
		dedup:    &fakeDedup{},
		state:    &fakeSharedState{allow: true},
	}
	h.wfRepo.workflows["wf1"] = webhookWorkflow()
	h.webhooks.put(&workflow.WorkflowWebhook{
		ID: "whid", WorkflowID: "wf1", WorkspaceID: "ws1", Token: "tok",
		AuthMode: workflow.WebhookAuthNone, Method: "POST", Active: true,
	})
	h.uc = NewHandleWebhookTriggerUseCase(h.webhooks, h.wfRepo, h.runRepo, h.entries, h.resolver, h.launcher, h.dedup, h.state)
	return h
}

func jsonEntryBody() []byte {
	return []byte(`{"entry_id":"entry1","entry_type":"whatsapp","name":"Jo"}`)
}

func baseReq() WebhookRequest {
	return WebhookRequest{Token: "tok", Method: "POST", RawBody: jsonEntryBody(), Header: headerFunc(nil)}
}

func TestWebhookTrigger_Success(t *testing.T) {
	h := newWebhookHarness()
	res, err := h.uc.Execute(baseReq())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.RunID == "" || res.Duplicate || res.AlreadyRunning {
		t.Fatalf("expected fresh run, got %+v", res)
	}
	if len(h.launcher.launched) != 1 {
		t.Fatalf("expected launcher called once, got %d", len(h.launcher.launched))
	}
	run := h.launcher.launched[0]
	if run.TriggerNodeID != "twh" || run.CurrentNodeID != "twh" {
		t.Fatalf("run must start at webhook trigger node, got trigger=%s current=%s", run.TriggerNodeID, run.CurrentNodeID)
	}
	if run.EntryID != "entry1" || run.EntryType != "whatsapp" {
		t.Fatalf("run entry mismatch: %s/%s", run.EntryID, run.EntryType)
	}
	webhookVar, ok := run.State.Get("webhook")
	if !ok {
		t.Fatal("expected webhook var seeded into run state")
	}
	vars, _ := webhookVar.(map[string]interface{})
	if vars["method"] != "POST" {
		t.Fatalf("expected method var, got %v", vars["method"])
	}
	body, _ := vars["body"].(map[string]interface{})
	if body["name"] != "Jo" {
		t.Fatalf("expected parsed body var, got %v", vars["body"])
	}
}

func TestWebhookTrigger_TokenNotFound(t *testing.T) {
	h := newWebhookHarness()
	req := baseReq()
	req.Token = "nope"
	if _, err := h.uc.Execute(req); !errors.Is(err, workflow.ErrWebhookNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestWebhookTrigger_Inactive(t *testing.T) {
	h := newWebhookHarness()
	h.webhooks.byToken["tok"].Active = false
	if _, err := h.uc.Execute(baseReq()); !errors.Is(err, workflow.ErrWebhookNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestWebhookTrigger_FindByTokenError(t *testing.T) {
	h := newWebhookHarness()
	sentinel := errors.New("db down")
	h.webhooks.findErr = sentinel
	if _, err := h.uc.Execute(baseReq()); !errors.Is(err, sentinel) {
		t.Fatalf("got %v", err)
	}
}

func TestWebhookTrigger_MethodNotAllowed(t *testing.T) {
	h := newWebhookHarness()
	req := baseReq()
	req.Method = "GET"
	if _, err := h.uc.Execute(req); !errors.Is(err, workflow.ErrWebhookMethodNotAllowed) {
		t.Fatalf("got %v", err)
	}
}

func TestWebhookTrigger_HeaderTokenAuth(t *testing.T) {
	h := newWebhookHarness()
	h.webhooks.byToken["tok"].AuthMode = workflow.WebhookAuthHeaderToken
	h.webhooks.byToken["tok"].HeaderName = "X-Token"
	h.webhooks.byToken["tok"].Secret = "shh"

	req := baseReq()
	req.Header = headerFunc(map[string]string{"X-Token": "shh"})
	if _, err := h.uc.Execute(req); err != nil {
		t.Fatalf("valid token should pass: %v", err)
	}

	bad := baseReq()
	bad.Header = headerFunc(map[string]string{"X-Token": "wrong"})
	if _, err := h.uc.Execute(bad); !errors.Is(err, workflow.ErrWebhookUnauthorized) {
		t.Fatalf("got %v", err)
	}

	h.webhooks.byToken["tok"].Secret = ""
	empty := baseReq()
	empty.Header = headerFunc(map[string]string{"X-Token": ""})
	if _, err := h.uc.Execute(empty); !errors.Is(err, workflow.ErrWebhookUnauthorized) {
		t.Fatalf("empty configured secret must not authorize: %v", err)
	}
}

func TestWebhookTrigger_HMACAuth(t *testing.T) {
	h := newWebhookHarness()
	h.webhooks.byToken["tok"].AuthMode = workflow.WebhookAuthHMAC
	h.webhooks.byToken["tok"].HeaderName = "X-Sig"
	h.webhooks.byToken["tok"].Secret = "sekret"

	body := jsonEntryBody()
	mac := hmac.New(sha256.New, []byte("sekret"))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	ok := WebhookRequest{Token: "tok", Method: "POST", RawBody: body, Header: headerFunc(map[string]string{"X-Sig": sig})}
	if _, err := h.uc.Execute(ok); err != nil {
		t.Fatalf("valid hmac should pass: %v", err)
	}

	bad := WebhookRequest{Token: "tok", Method: "POST", RawBody: body, Header: headerFunc(map[string]string{"X-Sig": "sha256=deadbeef"})}
	if _, err := h.uc.Execute(bad); !errors.Is(err, workflow.ErrWebhookUnauthorized) {
		t.Fatalf("got %v", err)
	}
}

func TestWebhookTrigger_Duplicate(t *testing.T) {
	h := newWebhookHarness()
	h.dedup.duplicate = true
	res, err := h.uc.Execute(baseReq())
	if err != nil {
		t.Fatalf("duplicate should not error: %v", err)
	}
	if !res.Duplicate {
		t.Fatal("expected duplicate result")
	}
	if len(h.launcher.launched) != 0 {
		t.Fatal("duplicate must not launch a run")
	}
}

func TestWebhookTrigger_DedupKeyPrefersHeader(t *testing.T) {
	h := newWebhookHarness()
	req := baseReq()
	req.Header = headerFunc(map[string]string{"X-Idempotency-Key": "evt-42"})
	if _, err := h.uc.Execute(req); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(h.dedup.keys) != 1 || h.dedup.keys[0] != "wfwh:tok:evt-42" {
		t.Fatalf("expected header-based dedup key, got %v", h.dedup.keys)
	}
}

func TestWebhookTrigger_EntryRequired(t *testing.T) {
	h := newWebhookHarness()
	req := baseReq()
	req.RawBody = []byte(`{"name":"no-entry"}`)
	if _, err := h.uc.Execute(req); !errors.Is(err, workflow.ErrWebhookEntryRequired) {
		t.Fatalf("got %v", err)
	}

	notJSON := baseReq()
	notJSON.RawBody = []byte("plain text")
	if _, err := h.uc.Execute(notJSON); !errors.Is(err, workflow.ErrWebhookEntryRequired) {
		t.Fatalf("non-json body should require entry: %v", err)
	}
}

func TestWebhookTrigger_EntryForbidden(t *testing.T) {
	h := newWebhookHarness()
	h.entries.owns = false
	if _, err := h.uc.Execute(baseReq()); !errors.Is(err, workflow.ErrWebhookEntryForbidden) {
		t.Fatalf("got %v", err)
	}
}

func TestWebhookTrigger_EntryCheckerError(t *testing.T) {
	h := newWebhookHarness()
	sentinel := errors.New("entry db error")
	h.entries.err = sentinel
	if _, err := h.uc.Execute(baseReq()); !errors.Is(err, sentinel) {
		t.Fatalf("got %v", err)
	}
}

func TestWebhookTrigger_WorkflowInactiveOrMissing(t *testing.T) {
	h := newWebhookHarness()
	h.wfRepo.workflows["wf1"].Status = workflow.WorkflowStatusPaused
	if _, err := h.uc.Execute(baseReq()); !errors.Is(err, workflow.ErrWorkflowNotActive) {
		t.Fatalf("paused: got %v", err)
	}

	delete(h.wfRepo.workflows, "wf1")
	if _, err := h.uc.Execute(baseReq()); !errors.Is(err, workflow.ErrWorkflowNotActive) {
		t.Fatalf("missing: got %v", err)
	}
}

func TestWebhookTrigger_NoTriggerNode(t *testing.T) {
	h := newWebhookHarness()
	h.wfRepo.workflows["wf1"].Graph.Nodes = []workflow.Node{
		{ID: "tm", Type: workflow.NodeTypeTriggerManual},
		{ID: "e1", Type: workflow.NodeTypeEnd},
	}
	if _, err := h.uc.Execute(baseReq()); !errors.Is(err, workflow.ErrWebhookNoTriggerNode) {
		t.Fatalf("got %v", err)
	}
}

func TestWebhookTrigger_AlreadyRunning(t *testing.T) {
	h := newWebhookHarness()
	h.runRepo.runs["r-existing"] = &workflow.WorkflowRun{
		ID: "r-existing", WorkflowID: "wf1", EntryID: "entry1", TriggerNodeID: "twh",
		Status: workflow.RunStatusRunning,
	}
	res, err := h.uc.Execute(baseReq())
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if !res.AlreadyRunning || res.RunID != "r-existing" {
		t.Fatalf("expected already-running existing run, got %+v", res)
	}
	if len(h.launcher.launched) != 0 {
		t.Fatal("must not launch a second run")
	}
}

func TestWebhookTrigger_IndependentFromOtherTrigger(t *testing.T) {
	h := newWebhookHarness()
	// An active run for the SAME entry but started by a DIFFERENT trigger
	// (message_received) must not block the webhook trigger's own run.
	h.runRepo.runs["msg-run"] = &workflow.WorkflowRun{
		ID: "msg-run", WorkflowID: "wf1", EntryID: "entry1", TriggerNodeID: "tmr",
		Status: workflow.RunStatusRunning,
	}
	res, err := h.uc.Execute(baseReq())
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if res.AlreadyRunning || res.RunID == "" {
		t.Fatalf("webhook must start its own independent run, got %+v", res)
	}
	if len(h.launcher.launched) != 1 {
		t.Fatal("expected an independent webhook run to launch")
	}
	if h.launcher.launched[0].TriggerNodeID != "twh" {
		t.Fatalf("independent run must be keyed to the webhook trigger, got %s", h.launcher.launched[0].TriggerNodeID)
	}
}

func TestWebhookTrigger_AtCapacity(t *testing.T) {
	h := newWebhookHarness()
	h.state.allow = false
	if _, err := h.uc.Execute(baseReq()); !errors.Is(err, workflow.ErrWorkspaceAtCapacity) {
		t.Fatalf("got %v", err)
	}
}

func TestWebhookTrigger_CreateErrorReleasesSlot(t *testing.T) {
	h := newWebhookHarness()
	h.runRepo.CreateErr = errors.New("insert failed")
	if _, err := h.uc.Execute(baseReq()); err == nil {
		t.Fatal("expected create error")
	}
	if h.state.decrs != 1 {
		t.Fatalf("slot must be released on create failure, decrs=%d", h.state.decrs)
	}
}

func TestWebhookTrigger_LookupErrorsPropagate(t *testing.T) {
	h := newWebhookHarness()
	h.runRepo.FindErr = errors.New("find failed")
	if _, err := h.uc.Execute(baseReq()); err == nil {
		t.Fatal("expected active-run lookup error to propagate")
	}
}

func TestWebhookTrigger_WorkflowLookupError(t *testing.T) {
	h := newWebhookHarness()
	h.wfRepo.FindErr = errors.New("wf lookup failed")
	if _, err := h.uc.Execute(baseReq()); err == nil {
		t.Fatal("expected workflow lookup error to propagate")
	}
}

func TestWebhookTrigger_EmptyBody(t *testing.T) {
	h := newWebhookHarness()
	req := baseReq()
	req.RawBody = nil
	if _, err := h.uc.Execute(req); !errors.Is(err, workflow.ErrWebhookEntryRequired) {
		t.Fatalf("empty body should require entry: %v", err)
	}
}
