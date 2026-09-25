package copilottools

import (
	"context"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/label"
	"vozko/domain/shared"
)

const knownLabel = "5d4c3b2a-1f0e-4d9c-8b7a-6f5e4d3c2b1a"

type fakeEntryLabels struct {
	by      shared.Person
	applied []label.AssignEntryLabelInput
	removed []label.RemoveEntryLabelInput
	err     error
}

func (f *fakeEntryLabels) Apply(_ string, by shared.Person, in label.AssignEntryLabelInput) (*label.EntryLabel, error) {
	f.by = by
	f.applied = append(f.applied, in)
	return &label.EntryLabel{LabelID: in.LabelID}, f.err
}

func (f *fakeEntryLabels) Remove(_ string, by shared.Person, in label.RemoveEntryLabelInput) error {
	f.by = by
	f.removed = append(f.removed, in)
	return f.err
}

type fakeCreateLabel struct{ created []label.CreateLabelInput }

func (f *fakeCreateLabel) Execute(_ string, in label.CreateLabelInput) (*label.Label, error) {
	f.created = append(f.created, in)
	return &label.Label{ID: knownLabel, Name: in.Name}, nil
}

type fakeLabelList struct{}

func (fakeLabelList) Execute(string) ([]*label.Label, error) {
	return []*label.Label{{ID: knownLabel, Name: "VIP"}}, nil
}

type fakeLabelBroadcast struct{ sent int }

func (f *fakeLabelBroadcast) BroadcastLabelUpdate(string, string, string) { f.sent++ }

func labelDeps(entries *fakeEntryLabels, create *fakeCreateLabel, broadcast *fakeLabelBroadcast) LabelDeps {
	return LabelDeps{EntryLabels: entries, Create: create, List: fakeLabelList{}, Entries: &fakeEntries{}, Broadcast: broadcast}
}

func labelArgs() map[string]interface{} {
	return map[string]interface{}{"entry_id": knownEntry, "entry_type": "whatsapp", "label_id": knownLabel}
}

func TestLabelToolsNeedApprovalAndTheirRoutesPermissions(t *testing.T) {
	deps := labelDeps(&fakeEntryLabels{}, &fakeCreateLabel{}, &fakeLabelBroadcast{})
	want := map[string]string{"apply_label": "labels:assign", "remove_label": "labels:assign", "create_label": "labels:create"}
	for _, tool := range []copilot.Tool{NewApplyLabelTool(deps), NewRemoveLabelTool(deps), NewCreateLabelTool(deps)} {
		m := tool.Meta()
		if got := string(m.Resource) + ":" + string(m.Action); !m.Mutating || got != want[tool.Definition().Name] {
			t.Fatalf("%s meta = %+v", tool.Definition().Name, m)
		}
	}
}

func TestApplyLabelLabelsAsTheUserAndNotifiesTheInbox(t *testing.T) {
	entries, broadcast := &fakeEntryLabels{}, &fakeLabelBroadcast{}
	res := NewApplyLabelTool(labelDeps(entries, &fakeCreateLabel{}, broadcast)).Execute(context.Background(), member(), labelArgs())
	if res.Status != copilot.StatusOK || entries.by.UserID != "u-1" || entries.applied[0].LabelID != knownLabel || broadcast.sent != 1 {
		t.Fatalf("status %s by %+v applied %+v sent %d", res.Status, entries.by, entries.applied, broadcast.sent)
	}
}

func TestRemoveLabelRefusesAConversationTheUserCannotSee(t *testing.T) {
	entries, broadcast := &fakeEntryLabels{err: label.ErrEntryAccess}, &fakeLabelBroadcast{}
	res := NewRemoveLabelTool(labelDeps(entries, &fakeCreateLabel{}, broadcast)).Execute(context.Background(), member(), labelArgs())
	if res.Status != copilot.StatusDenied || broadcast.sent != 0 {
		t.Fatalf("status %s sent %d", res.Status, broadcast.sent)
	}
}

func TestLabelToolsDescribeByName(t *testing.T) {
	fields := NewApplyLabelTool(labelDeps(&fakeEntryLabels{}, &fakeCreateLabel{}, &fakeLabelBroadcast{})).(copilot.Describer).Describe(context.Background(), member(), labelArgs())
	got := map[string]string{}
	for _, f := range fields {
		got[f.Key] = f.Value
	}
	if got["conversation"] != "Maria (••••9624)" || got["label"] != "VIP" {
		t.Fatalf("fields = %v", fields)
	}
}

func TestCreateLabelCreatesInTheWorkspace(t *testing.T) {
	create := &fakeCreateLabel{}
	res := NewCreateLabelTool(labelDeps(&fakeEntryLabels{}, create, &fakeLabelBroadcast{})).Execute(context.Background(), member(), map[string]interface{}{"name": "Retornar"})
	if res.Status != copilot.StatusOK || create.created[0].Name != "Retornar" {
		t.Fatalf("status %s created %+v", res.Status, create.created)
	}
}

func TestLabelToolsRefuseInventedIds(t *testing.T) {
	entries := &fakeEntryLabels{}
	args := labelArgs()
	args["label_id"] = "vip"
	if res := NewApplyLabelTool(labelDeps(entries, &fakeCreateLabel{}, &fakeLabelBroadcast{})).Execute(context.Background(), member(), args); res.Status != copilot.StatusError || len(entries.applied) != 0 {
		t.Fatalf("status %s applied %v", res.Status, entries.applied)
	}
}
