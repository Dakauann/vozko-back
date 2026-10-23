package rcs

import (
	"reflect"
	"strings"
	"testing"

	"vozko/domain/channel"
	"vozko/domain/shared"
)

func TestDescriptor(t *testing.T) {
	d := Descriptor()
	if d.Kind != channel.KindRCS || d.EntryType != shared.EntryTypeRCS {
		t.Fatalf("descriptor identity = %s/%s", d.Kind, d.EntryType)
	}
	if !reflect.DeepEqual(d.Capabilities, Capabilities()) {
		t.Fatalf("the descriptor must carry the channel capabilities: %+v", d.Capabilities)
	}
	sql := d.InboxSQL
	if sql.EntryTable != "rcs_conversations" || !strings.Contains(sql.EntryJoin, "JOIN rcs_senders ") || !strings.Contains(sql.ContainerNameField, "brand_name") {
		t.Fatalf("inbox sql = %+v", sql)
	}
	if !strings.Contains(sql.AccountIDField, "sender_id") || !strings.Contains(sql.ContactIDField, "contact_id") {
		t.Fatalf("inbox sql fields = %+v", sql)
	}
	for _, column := range []string{"agent_id", "workflow_id", "enable_agent_responses AS agent_responses_enabled", "enable_workflow AS workflow_enabled"} {
		if !strings.Contains(sql.AutomationFields, column) {
			t.Fatalf("automation fields miss %s: %s", column, sql.AutomationFields)
		}
	}
}
