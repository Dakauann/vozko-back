package report_renderers

import (
	"testing"

	"vozko/domain/export"
	"vozko/domain/workspace"
)

func TestEveryExportableEntryTypeHasAReadPolicy(t *testing.T) {
	if len(export.ExportableEntryTypes()) == 0 {
		t.Fatal("no exportable entry types")
	}
	r := NewConversationEntriesRenderer(&capturingExporter{}, scopesOf{})
	for _, entryType := range export.ExportableEntryTypes() {
		if _, ok := entryResources[entryType]; !ok {
			t.Errorf("%s is exportable but has no read resource; its entries report would be refused", entryType)
			continue
		}
		if _, err := r.Policy(entriesJob(t, "u", entryType)); err != nil {
			t.Errorf("%s: policy err = %v", entryType, err)
		}
	}
}

func TestWebchatEntriesRequireTheWidgetReadPermission(t *testing.T) {
	r := NewConversationEntriesRenderer(&capturingExporter{}, scopesOf{})
	policy, err := r.Policy(entriesJob(t, "u", export.EntryTypeWebchat))
	if err != nil {
		t.Fatal(err)
	}
	if len(policy.Required) != 1 || policy.Required[0] != (workspace.PermissionEntry{Resource: workspace.ResourceWebchatWidgets, Action: workspace.ActionRead}) || !policy.RequesterOnly {
		t.Fatalf("policy = %+v", policy)
	}
}
