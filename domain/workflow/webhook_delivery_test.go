package workflow

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestEveryWebhookRejectionHasAStableReason(t *testing.T) {
	cases := []struct {
		err    error
		reason string
	}{
		{ErrWebhookInactive, "webhook_inactive"},
		{ErrWebhookNotFound, "webhook_not_found"},
		{ErrWebhookMethodNotAllowed, "method_not_allowed"},
		{ErrWebhookUnauthorized, "unauthorized"},
		{ErrWebhookEntryRequired, "entry_required"},
		{ErrWebhookEntryNotFound, "entry_not_found"},
		{ErrWebhookEntryForbidden, "entry_forbidden"},
		{ErrWebhookNoTriggerNode, "no_trigger_node"},
		{ErrWorkflowNotActive, "workflow_not_active"},
		{ErrWorkspaceAtCapacity, "workspace_at_capacity"},
		{fmt.Errorf("lookup: %w", ErrWorkflowNotActive), "workflow_not_active"},
		{errors.New("db down"), WebhookReasonInternal},
	}
	for _, tc := range cases {
		if reason := WebhookRejectionReason(tc.err); reason != tc.reason {
			t.Errorf("WebhookRejectionReason(%v) = %s, want %s", tc.err, reason, tc.reason)
		}
	}
}

func TestAnInactiveWebhookStillLooksNotFoundToTheCaller(t *testing.T) {
	if !errors.Is(ErrWebhookInactive, ErrWebhookNotFound) {
		t.Fatal("an inactive webhook must answer like a missing one so the token is not confirmed")
	}
}

func TestPayloadKeysKeepOnlyTopLevelKeysNeverValues(t *testing.T) {
	keys := PayloadKeys(map[string]interface{}{"phone": "+55119", "cpf": "529", "nested": map[string]interface{}{"inner": 1}})
	if strings.Join(keys, ",") != "cpf,nested,phone" {
		t.Fatalf("keys = %v", keys)
	}
	if PayloadKeys(nil) != nil {
		t.Fatal("no body has no keys")
	}
}

func TestPayloadKeysAreBoundedSoAPayloadCannotInflateTheLog(t *testing.T) {
	body := map[string]interface{}{strings.Repeat("k", 500): 1}
	for i := 0; i < 500; i++ {
		body[fmt.Sprintf("key_%03d", i)] = i
	}
	keys := PayloadKeys(body)
	if len(keys) != MaxPayloadKeys {
		t.Fatalf("kept %d keys, want %d", len(keys), MaxPayloadKeys)
	}
	for _, k := range keys {
		if len(k) > MaxPayloadKeyLength {
			t.Fatalf("key of %d bytes kept", len(k))
		}
	}
}
