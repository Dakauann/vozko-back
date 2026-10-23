package attendancetools

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/choicestats"
	"vozko/domain/copilot"
	"vozko/domain/conversation"
	uw "vozko/domain/unofficial_whatsapp"
	wc "vozko/domain/whatsapp_campaign"
	"vozko/domain/workflow"
	"vozko/domain/workspace"
	"vozko/usecases/copilot/copilottools/tooltest"
)

type fakeChoiceReader struct {
	calls   []string
	from    string
	to      string
	version *int
	access  wc.ReportAccess
	err     error
}

func (f *fakeChoiceReader) Workflow(_ context.Context, _ workflow.Scope, id, from, to string, version *int) (*choicestats.WorkflowReport, error) {
	f.calls, f.from, f.to, f.version = append(f.calls, "workflow:"+id), from, to, version
	return &choicestats.WorkflowReport{From: from, To: to, Nodes: []choicestats.NodeStats{}}, f.err
}

func (f *fakeChoiceReader) WhatsAppCampaign(_ context.Context, access wc.ReportAccess, id, from, to string) (*choicestats.PromptReport, error) {
	f.calls, f.access = append(f.calls, "whatsapp_campaign:"+id), access
	return &choicestats.PromptReport{}, f.err
}

func (f *fakeChoiceReader) UnofficialCampaign(_ context.Context, _ string, _ uw.DepartmentScope, id, from, to string) (*choicestats.PromptReport, error) {
	f.calls = append(f.calls, "unofficial_campaign:"+id)
	return &choicestats.PromptReport{}, f.err
}

func (f *fakeChoiceReader) Template(_ context.Context, _ string, id, from, to string) (*choicestats.PromptReport, error) {
	f.calls, f.from, f.to = append(f.calls, "template:"+id), from, to
	return &choicestats.PromptReport{}, f.err
}

type fakePermissions struct{ denied workspace.Resource }

func (f fakePermissions) Execute(_, _ string, resource workspace.Resource, _ workspace.Action) error {
	if resource == f.denied {
		return errors.New("denied")
	}
	return nil
}

type everyScope struct{}

func (everyScope) GetDepartmentScope(string, string, bool) (conversation.DepartmentAccessScope, bool) {
	return conversation.DepartmentAccessScope{}, true
}

func choiceTool(reader *fakeChoiceReader, perms fakePermissions) copilot.Tool {
	return NewReadChoiceStatsTool(ChoiceStatsDeps{Reader: reader, Permissions: perms, Scopes: everyScope{}, Now: fixedNow})
}

func TestReadChoiceStatsReadsEachSubjectThroughTheSameUseCase(t *testing.T) {
	reader := &fakeChoiceReader{}
	tool := choiceTool(reader, fakePermissions{})
	if m := tool.Meta(); m.Resource != workspace.ResourceWorkflows || m.Action != workspace.ActionRead || m.Mutating || tool.Definition().Name != "read_choice_stats" {
		t.Fatalf("meta = %+v", m)
	}
	cc := tooltest.Member()
	for _, subject := range []string{"workflow", "whatsapp_campaign", "unofficial_campaign", "template"} {
		res := tool.Execute(context.Background(), cc, map[string]interface{}{"subject": subject, "id": "x-1", "version": float64(2)})
		if res.Status != copilot.StatusOK {
			t.Fatalf("%s: status = %v (%s)", subject, res.Status, res.Message)
		}
	}
	if len(reader.calls) != 4 || reader.calls[0] != "workflow:x-1" || reader.calls[3] != "template:x-1" {
		t.Fatalf("calls = %v", reader.calls)
	}
	if reader.access.AllowDepartment == nil || reader.access.WorkspaceID != "ws-1" {
		t.Fatalf("campaign access = %+v", reader.access)
	}
	if reader.from != "" || reader.to != "" {
		t.Fatalf("a template without dates reads its lifetime, got %q..%q", reader.from, reader.to)
	}
}

func TestReadChoiceStatsGivesTheWorkflowAPeriodAndAVersion(t *testing.T) {
	reader := &fakeChoiceReader{}
	res := choiceTool(reader, fakePermissions{}).Execute(context.Background(), tooltest.Member(), map[string]interface{}{"subject": "workflow", "id": "wf-1", "version": float64(3)})
	if res.Status != copilot.StatusOK || reader.from == "" || reader.to == "" || reader.version == nil || *reader.version != 3 {
		t.Fatalf("status = %v from = %q to = %q version = %v", res.Status, reader.from, reader.to, reader.version)
	}
}

func TestReadChoiceStatsRefusesWhatTheUserCannotSeeOrAsk(t *testing.T) {
	cases := []struct {
		name  string
		args  map[string]interface{}
		perms fakePermissions
		err   error
		want  copilot.Status
	}{
		{"a campaign without campaign access", map[string]interface{}{"subject": "whatsapp_campaign", "id": "c"}, fakePermissions{denied: workspace.ResourceWhatsAppCampaigns}, nil, copilot.StatusDenied},
		{"a template without template access", map[string]interface{}{"subject": "template", "id": "t"}, fakePermissions{denied: workspace.ResourceWhatsAppTemplates}, nil, copilot.StatusDenied},
		{"an unknown subject", map[string]interface{}{"subject": "sms", "id": "x"}, fakePermissions{}, nil, copilot.StatusError},
		{"no id", map[string]interface{}{"subject": "template"}, fakePermissions{}, nil, copilot.StatusError},
		{"a bad period", map[string]interface{}{"subject": "template", "id": "t"}, fakePermissions{}, choicestats.ErrInvalidPeriod, copilot.StatusError},
		{"a hidden workflow", map[string]interface{}{"subject": "workflow", "id": "w"}, fakePermissions{}, workflow.ErrWorkflowAccessDenied, copilot.StatusDenied},
		{"unreadable counters", map[string]interface{}{"subject": "template", "id": "t"}, fakePermissions{}, choicestats.ErrUnavailable, copilot.StatusError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reader := &fakeChoiceReader{err: tc.err}
			res := choiceTool(reader, tc.perms).Execute(context.Background(), tooltest.Member(), tc.args)
			if res.Status != tc.want || res.Message == "" {
				t.Fatalf("status = %v message = %q", res.Status, res.Message)
			}
		})
	}
}
