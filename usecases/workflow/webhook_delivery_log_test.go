package workflow_usecase

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

type fakeDeliveryLog struct {
	mu        sync.Mutex
	recorded  []workflow.WebhookDelivery
	recordErr error
}

func (f *fakeDeliveryLog) Record(d *workflow.WebhookDelivery) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.recordErr != nil {
		return f.recordErr
	}
	f.recorded = append(f.recorded, *d)
	return nil
}

func (f *fakeDeliveryLog) ListByWorkflow(workspaceID, workflowID string, _ conversation.VisibilityScope, pagination shared.Pagination) (*shared.PaginatedResult[*workflow.WebhookDelivery], error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	items := []*workflow.WebhookDelivery{}
	for i := range f.recorded {
		if f.recorded[i].WorkspaceID == workspaceID && f.recorded[i].WorkflowID == workflowID {
			cp := f.recorded[i]
			items = append(items, &cp)
		}
	}
	return shared.NewPaginatedResult(items, pagination, int64(len(items))), nil
}

func (f *fakeDeliveryLog) DeleteReceivedBefore(time.Time, int) (int64, error) { return 0, nil }

func (f *fakeDeliveryLog) only(t *testing.T) workflow.WebhookDelivery {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.recorded) != 1 {
		t.Fatalf("recorded %d deliveries, want 1: %+v", len(f.recorded), f.recorded)
	}
	return f.recorded[0]
}

func TestAnAcceptedDeliveryIsLoggedAndItsRunNamesTheDelivery(t *testing.T) {
	h := newWebhookHarness()
	res, err := h.uc.Execute(baseReq())
	if err != nil {
		t.Fatal(err)
	}
	d := h.deliveries.only(t)
	if d.Outcome != workflow.WebhookOutcomeAccepted || d.HTTPStatus != http.StatusAccepted || d.RunID != res.RunID {
		t.Fatalf("delivery = %+v", d)
	}
	if d.WebhookID != "whid" || d.WorkflowID != "wf1" || d.WorkspaceID != "ws1" || d.ID == "" || d.ReceivedAt.IsZero() || d.LatencyMs < 0 {
		t.Fatalf("delivery identity = %+v", d)
	}
	if d.EntryResolution != workflow.WebhookEntryByID || d.EntryID != "entry1" || d.EntryType != "whatsapp" {
		t.Fatalf("entry = %+v", d)
	}
	if strings.Join(d.PayloadKeys, ",") != "entry_id,entry_type,name" {
		t.Fatalf("payload keys = %v", d.PayloadKeys)
	}
	run := h.launcher.launched[0]
	if run.StartedBy != workflow.StartedByWebhook(d.ID) || run.TriggerType != workflow.TriggerWebhook {
		t.Fatalf("run started by %q (%s), want the delivery %s", run.StartedBy, run.TriggerType, d.ID)
	}
	if strings.Join(h.metrics.deliveries, ",") != "accepted/" {
		t.Fatalf("metrics = %v", h.metrics.deliveries)
	}
}

func TestAPhoneResolvedDeliverySaysSo(t *testing.T) {
	h := newWebhookHarness()
	h.resolver.entryID, h.resolver.entryType = "entry-9", "whatsapp"
	req := baseReq()
	req.RawBody = []byte(`{"phone":"+5511999990000","cpf":"52998224725"}`)
	if _, err := h.uc.Execute(req); err != nil {
		t.Fatal(err)
	}
	d := h.deliveries.only(t)
	if d.EntryResolution != workflow.WebhookEntryByPhone || d.EntryID != "entry-9" {
		t.Fatalf("delivery = %+v", d)
	}
	if strings.Contains(strings.Join(d.PayloadKeys, ","), "52998224725") {
		t.Fatal("payload values must never be logged")
	}
}

func TestEveryRejectionOfAKnownWebhookIsLoggedWithItsReason(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(h *webhookHarness, req *WebhookRequest)
		reason  string
		status  int
	}{
		{"inactive webhook", func(h *webhookHarness, _ *WebhookRequest) { h.webhooks.byToken["tok"].Active = false }, "webhook_inactive", http.StatusNotFound},
		{"wrong method", func(_ *webhookHarness, req *WebhookRequest) { req.Method = "GET" }, "method_not_allowed", http.StatusMethodNotAllowed},
		{"workflow saved as draft", func(h *webhookHarness, _ *WebhookRequest) {
			h.wfRepo.workflows["wf1"].Status = workflow.WorkflowStatusDraft
		}, "workflow_not_active", http.StatusConflict},
		{"phone with no conversation", func(_ *webhookHarness, req *WebhookRequest) { req.RawBody = []byte(`{"phone":"+55"}`) }, "entry_not_found", http.StatusNotFound},
		{"no entry at all", func(_ *webhookHarness, req *WebhookRequest) { req.RawBody = []byte(`{}`) }, "entry_required", http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newWebhookHarness()
			req := baseReq()
			tc.prepare(h, &req)
			if _, err := h.uc.Execute(req); err == nil {
				t.Fatal("expected a rejection")
			}
			d := h.deliveries.only(t)
			if d.Outcome != workflow.WebhookOutcomeRejected || d.Reason != tc.reason || d.HTTPStatus != tc.status || d.RunID != "" {
				t.Fatalf("delivery = %+v", d)
			}
			if got := h.metrics.deliveries; len(got) != 1 || got[0] != "rejected/"+tc.reason {
				t.Fatalf("metrics = %v", got)
			}
		})
	}
}

func TestAnUnauthorizedCallIsLoggedWithoutReadingItsPayload(t *testing.T) {
	h := newWebhookHarness()
	wh := h.webhooks.byToken["tok"]
	wh.AuthMode, wh.Secret, wh.HeaderName = workflow.WebhookAuthHeaderToken, "s3cret", "X-Webhook-Token"
	if _, err := h.uc.Execute(baseReq()); !errors.Is(err, workflow.ErrWebhookUnauthorized) {
		t.Fatalf("err = %v", err)
	}
	d := h.deliveries.only(t)
	if d.Reason != "unauthorized" || d.HTTPStatus != http.StatusUnauthorized || d.PayloadKeys != nil {
		t.Fatalf("delivery = %+v", d)
	}
}

func TestDuplicateAndAlreadyRunningDeliveriesAreLogged(t *testing.T) {
	h := newWebhookHarness()
	h.dedup.duplicate = true
	if _, err := h.uc.Execute(baseReq()); err != nil {
		t.Fatal(err)
	}
	if d := h.deliveries.only(t); d.Outcome != workflow.WebhookOutcomeDuplicate || d.HTTPStatus != http.StatusOK {
		t.Fatalf("delivery = %+v", d)
	}

	h2 := newWebhookHarness()
	existing := &workflow.WorkflowRun{ID: "run-live", WorkflowID: "wf1", EntryID: "entry1", TriggerNodeID: "twh", Status: workflow.RunStatusWaiting}
	_ = h2.runRepo.Create(existing)
	if _, err := h2.uc.Execute(baseReq()); err != nil {
		t.Fatal(err)
	}
	if d := h2.deliveries.only(t); d.Outcome != workflow.WebhookOutcomeAlreadyRunning || d.RunID != "run-live" {
		t.Fatalf("delivery = %+v", d)
	}
}

func TestAnUnknownTokenIsOnlyCountedBecauseNoWorkflowOwnsIt(t *testing.T) {
	h := newWebhookHarness()
	req := baseReq()
	req.Token = "nope"
	if _, err := h.uc.Execute(req); !errors.Is(err, workflow.ErrWebhookNotFound) {
		t.Fatalf("err = %v", err)
	}
	if len(h.deliveries.recorded) != 0 {
		t.Fatalf("an unattributable call was logged: %+v", h.deliveries.recorded)
	}
	if got := h.metrics.deliveries; len(got) != 1 || got[0] != "rejected/webhook_not_found" {
		t.Fatalf("metrics = %v", got)
	}
}

func TestAFailedDeliveryLogNeverChangesTheAnswer(t *testing.T) {
	h := newWebhookHarness()
	h.deliveries.recordErr = errors.New("db down")
	res, err := h.uc.Execute(baseReq())
	if err != nil || res.RunID == "" {
		t.Fatalf("res = %+v err = %v, the run must start even when the log write fails", res, err)
	}
}

func TestDeliveriesAreListedForTheCallersWorkspaceOnly(t *testing.T) {
	h := newWebhookHarness()
	if _, err := h.uc.Execute(baseReq()); err != nil {
		t.Fatal(err)
	}
	list := NewListWebhookDeliveriesUseCase(h.deliveries, everyoneVisible)
	mine, err := list.Execute(viewer, "ws1", "wf1", shared.Pagination{Page: 1, PageSize: 20})
	if err != nil || len(mine.Items) != 1 {
		t.Fatalf("mine = %+v err = %v", mine, err)
	}
	theirs, err := list.Execute(viewer, "ws-other", "wf1", shared.Pagination{Page: 1, PageSize: 20})
	if err != nil || len(theirs.Items) != 0 {
		t.Fatalf("another workspace saw %+v (err %v)", theirs, err)
	}
	if _, err := list.Execute(viewer, "", "wf1", shared.Pagination{}); !errors.Is(err, workflow.ErrWorkspaceIDRequired) {
		t.Fatalf("no workspace must be refused, got %v", err)
	}
}
