package workflow_usecase

import (
	"errors"
	"testing"

	"vozko/domain/workflow"
)

func phoneReq(body string) WebhookRequest {
	return WebhookRequest{Token: "tok", Method: "POST", RawBody: []byte(body), Header: headerFunc(nil)}
}

func TestWebhookTrigger_ResolvesByPhone(t *testing.T) {
	h := newWebhookHarness()
	h.resolver.entryID = "e-ph"
	h.resolver.entryType = "whatsapp"

	res, err := h.uc.Execute(phoneReq(`{"phone":"+55 11 99888-7777"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.RunID == "" || res.AlreadyRunning || res.Duplicate {
		t.Fatalf("expected a fresh run, got %+v", res)
	}
	if len(h.launcher.launched) != 1 {
		t.Fatalf("expected one launch, got %d", len(h.launcher.launched))
	}
	run := h.launcher.launched[0]
	if run.EntryID != "e-ph" || run.EntryType != "whatsapp" {
		t.Fatalf("run must use the resolved entry, got %s/%s", run.EntryID, run.EntryType)
	}
	if len(h.resolver.seen) != 1 || h.resolver.seen[0] != "ws1/+55 11 99888-7777" {
		t.Fatalf("resolver must be called with workspace and raw phone, got %v", h.resolver.seen)
	}
	if len(h.entries.seen) != 0 {
		t.Fatalf("ownership check must be skipped on the phone path, got %v", h.entries.seen)
	}
}

func TestWebhookTrigger_PhoneNotFound(t *testing.T) {
	h := newWebhookHarness()

	_, err := h.uc.Execute(phoneReq(`{"phone":"+5511999999999"}`))
	if !errors.Is(err, workflow.ErrWebhookEntryNotFound) {
		t.Fatalf("expected ErrWebhookEntryNotFound, got %v", err)
	}
	if len(h.launcher.launched) != 0 {
		t.Fatalf("no run should launch when the phone matches nothing")
	}
}

func TestWebhookTrigger_NoEntryNoPhone(t *testing.T) {
	h := newWebhookHarness()

	_, err := h.uc.Execute(phoneReq(`{"foo":"bar"}`))
	if !errors.Is(err, workflow.ErrWebhookEntryRequired) {
		t.Fatalf("expected ErrWebhookEntryRequired, got %v", err)
	}
}

func TestWebhookTrigger_EntryIDSkipsResolver(t *testing.T) {
	h := newWebhookHarness()

	if _, err := h.uc.Execute(baseReq()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(h.resolver.seen) != 0 {
		t.Fatalf("resolver must not be consulted when entry_id is present, got %v", h.resolver.seen)
	}
}
