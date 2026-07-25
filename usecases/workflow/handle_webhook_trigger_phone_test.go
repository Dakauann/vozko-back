package workflow_usecase

import (
	"errors"
	"testing"

	"vozko/domain/workflow"
)

func phoneReq(body string) WebhookRequest {
	return WebhookRequest{Token: "tok", Method: "POST", RawBody: []byte(body), Header: headerFunc(nil)}
}

// A payload with no entry_id but a phone resolves the entry via the resolver and
// starts the run against the resolved entry, without touching the ownership
// check (resolution is already workspace-scoped).
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

// A phone that matches no entry is a 404-class outcome, and no run is started.
func TestWebhookTrigger_PhoneNotFound(t *testing.T) {
	h := newWebhookHarness() // resolver returns "" by default

	_, err := h.uc.Execute(phoneReq(`{"phone":"+5511999999999"}`))
	if !errors.Is(err, workflow.ErrWebhookEntryNotFound) {
		t.Fatalf("expected ErrWebhookEntryNotFound, got %v", err)
	}
	if len(h.launcher.launched) != 0 {
		t.Fatalf("no run should launch when the phone matches nothing")
	}
}

// Neither entry_id nor phone is a bad request.
func TestWebhookTrigger_NoEntryNoPhone(t *testing.T) {
	h := newWebhookHarness()

	_, err := h.uc.Execute(phoneReq(`{"foo":"bar"}`))
	if !errors.Is(err, workflow.ErrWebhookEntryRequired) {
		t.Fatalf("expected ErrWebhookEntryRequired, got %v", err)
	}
}

// The pre-existing entry_id contract is untouched: when entry_id is present the
// resolver is never consulted.
func TestWebhookTrigger_EntryIDSkipsResolver(t *testing.T) {
	h := newWebhookHarness()

	if _, err := h.uc.Execute(baseReq()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(h.resolver.seen) != 0 {
		t.Fatalf("resolver must not be consulted when entry_id is present, got %v", h.resolver.seen)
	}
}
