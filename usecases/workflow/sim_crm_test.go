package workflow_usecase

import (
	"errors"
	"testing"

	label_domain "vozko/domain/label"
	stage_domain "vozko/domain/stage"
	"vozko/domain/workflow"
	workspace_domain "vozko/domain/workspace"
	dept_domain "vozko/domain/workspace/workspace_department"
	lead_usecase "vozko/usecases/lead"
	opportunity_usecase "vozko/usecases/opportunity"
)

type crmLabels struct {
	label_domain.Repository
	writes int
}

func (l *crmLabels) FindByID(id string) (*label_domain.Label, error) {
	labels := map[string]*label_domain.Label{
		"l-vip":     {ID: "l-vip", WorkspaceID: "ws1", Name: "VIP"},
		"l-foreign": {ID: "l-foreign", WorkspaceID: "ws2", Name: "Outro"},
	}
	if found, ok := labels[id]; ok {
		return found, nil
	}
	return nil, label_domain.ErrLabelNotFound
}

func (l *crmLabels) AssignLabel(*label_domain.EntryLabel) error {
	l.writes++
	return nil
}

type crmStages struct{}

func (crmStages) FindByID(id string) (*stage_domain.Stage, error) {
	if id == "st-proposta" {
		return &stage_domain.Stage{ID: id, WorkspaceID: "ws1", Name: "Proposta", PipelineID: "p1"}, nil
	}
	return nil, stage_domain.ErrTagNotFound
}

func (crmStages) GetEntryStage(string, string, string) (*stage_domain.EntryStage, error) {
	return nil, nil
}

type crmMembers struct {
	workspace_domain.Repository
}

func (crmMembers) GetMember(workspaceID, userID string) (*workspace_domain.Member, error) {
	if workspaceID == "ws1" && userID == "u-ana" {
		return &workspace_domain.Member{UserID: "u-ana", WorkspaceID: "ws1", Email: "ana@example.com"}, nil
	}
	return nil, errors.New("member not found")
}

type crmDepartments struct {
	dept_domain.Repository
}

func (crmDepartments) GetDepartmentByID(id string) (*dept_domain.Department, error) {
	if id == "d-fin" {
		return &dept_domain.Department{ID: id, WorkspaceID: "ws1", Name: "Financeiro"}, nil
	}
	return nil, errors.New("department not found")
}

type crmDealPreviews struct{}

func (crmDealPreviews) PreviewForEntry(workspaceID string, cmd opportunity_usecase.EntryCommand) (*opportunity_usecase.EntryPreview, error) {
	if workspaceID != "ws1" || cmd.PipelineID != "p-vendas" {
		return nil, opportunity_usecase.ErrPipelineNotFound
	}
	return &opportunity_usecase.EntryPreview{
		Action: cmd.Action, PipelineID: cmd.PipelineID, Title: cmd.Title, ValueCents: cmd.ValueCents,
		Stage: &stage_domain.Stage{ID: "st-ganho", Name: "Ganho", IsWon: true},
	}, nil
}

type crmLeadPreviews struct{}

func (crmLeadPreviews) Preview(in lead_usecase.ProfileUpdate) (lead_usecase.ProfilePreview, error) {
	if _, unknown := in.Profile.CustomFields["turno"]; unknown {
		return lead_usecase.ProfilePreview{}, errors.New("unknown field")
	}
	return lead_usecase.ProfilePreview{Changed: []string{"custom_fields.curso"}, Values: map[string]interface{}{"custom_fields.curso": "Direito"}}, nil
}

func crmDeps(labels *crmLabels) ExecutorDeps {
	return ExecutorDeps{
		LabelRepo: labels, StageRepo: crmStages{}, WorkspaceRepo: crmMembers{}, DepartmentRepo: crmDepartments{},
		DealPreviews: crmDealPreviews{}, LeadPreviews: crmLeadPreviews{},
	}
}

func TestACRMNodeInTheSandboxSaysWhatItWouldDoAndWritesNothing(t *testing.T) {
	cases := []struct {
		name   string
		node   workflow.Node
		kind   workflow.SimulatedChangeKind
		target string
	}{
		{"label", workflow.Node{ID: "n1", Type: workflow.NodeTypeActionAssignLabel, Config: map[string]interface{}{"action": "add", "label_id": "l-vip"}}, workflow.SimulatedLabel, "VIP"},
		{"stage", workflow.Node{ID: "n1", Type: workflow.NodeTypeActionMoveStage, Config: map[string]interface{}{"stage_id": "st-proposta"}}, workflow.SimulatedStage, "Proposta"},
		{"member", workflow.Node{ID: "n1", Type: workflow.NodeTypeActionAssignMember, Config: map[string]interface{}{"member_id": "u-ana"}}, workflow.SimulatedMember, "ana@example.com"},
		{"department", workflow.Node{ID: "n1", Type: workflow.NodeTypeActionTransferDepartment, Config: map[string]interface{}{"department_id": "d-fin"}}, workflow.SimulatedDepartment, "Financeiro"},
		{"deal", workflow.Node{ID: "n1", Type: workflow.NodeTypeActionManageOpportunity, Config: map[string]interface{}{"pipeline_id": "p-vendas", "action": "win", "title": "Plano", "value": "1500.50"}}, workflow.SimulatedDeal, "Ganho"},
		{"lead field", workflow.Node{ID: "n1", Type: workflow.NodeTypeActionUpdateLead, Config: map[string]interface{}{"custom_fields": map[string]interface{}{"curso": "Direito"}}}, workflow.SimulatedLeadField, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			labels := &crmLabels{}
			box := newSandboxRun(crmDeps(labels))
			result := box.execute(t, tc.node)
			if result.Output["success"] != true {
				t.Fatalf("output = %v, want the would-do success", result.Output)
			}
			changes := box.effects.drain().Changes
			if len(changes) != 1 || changes[0].Kind != tc.kind || changes[0].TargetName != tc.target || changes[0].Description == "" {
				t.Fatalf("changes = %+v", changes)
			}
			if labels.writes != 0 {
				t.Fatal("the sandbox wrote a label")
			}
		})
	}
}

type liveCRM struct {
	calls int
}

func (l *liveCRM) Change(string, label_domain.LabelChangeRequest) (label_domain.LabelChange, error) {
	l.calls++
	return label_domain.LabelChange{}, nil
}

func (l *liveCRM) Execute(string, stage_domain.AssignEntryStageInput) (*stage_domain.EntryStage, error) {
	l.calls++
	return &stage_domain.EntryStage{}, nil
}

func TestTheSandboxNeverUsesTheLiveCRMWritersItIsHanded(t *testing.T) {
	live := &liveCRM{}
	deps := crmDeps(&crmLabels{})
	deps.Labeler, deps.AssignStage = live, live
	nodes := []workflow.Node{
		{ID: "n1", Type: workflow.NodeTypeActionAssignLabel, Config: map[string]interface{}{"action": "add", "label_id": "l-vip"}},
		{ID: "n1", Type: workflow.NodeTypeActionMoveStage, Config: map[string]interface{}{"stage_id": "st-proposta"}},
	}
	for _, node := range nodes {
		box := newSandboxRun(deps)
		box.execute(t, node)
		if changes := box.effects.drain().Changes; len(changes) != 1 {
			t.Fatalf("%s: changes = %+v", node.Type, changes)
		}
	}
	if live.calls != 0 {
		t.Fatalf("the sandbox called a live CRM writer %d times", live.calls)
	}
}

func TestTheLeadAndDealWouldDoCarryTheValue(t *testing.T) {
	box := newSandboxRun(crmDeps(&crmLabels{}))
	box.execute(t, workflow.Node{ID: "n1", Type: workflow.NodeTypeActionUpdateLead, Config: map[string]interface{}{"custom_fields": map[string]interface{}{"curso": "Direito"}}})
	lead := box.effects.drain().Changes[0]
	if lead.Field != "custom_fields.curso" || lead.Value != "Direito" {
		t.Fatalf("lead change = %+v", lead)
	}

	box.execute(t, workflow.Node{ID: "n1", Type: workflow.NodeTypeActionManageOpportunity, Config: map[string]interface{}{"pipeline_id": "p-vendas", "action": "win", "title": "Plano", "value": "1500.50"}})
	deal := box.effects.drain().Changes[0]
	if deal.Field != "value_cents" || deal.Value != int64(150050) || deal.Action != "win" {
		t.Fatalf("deal change = %+v", deal)
	}
}

func TestACRMNodeWhoseTargetCannotBeResolvedFailsInTheSandbox(t *testing.T) {
	cases := []struct {
		name string
		deps func() ExecutorDeps
		node workflow.Node
	}{
		{"unknown label", func() ExecutorDeps { return crmDeps(&crmLabels{}) }, workflow.Node{ID: "n1", Type: workflow.NodeTypeActionAssignLabel, Config: map[string]interface{}{"label_id": "l-gone"}}},
		{"label of another workspace", func() ExecutorDeps { return crmDeps(&crmLabels{}) }, workflow.Node{ID: "n1", Type: workflow.NodeTypeActionAssignLabel, Config: map[string]interface{}{"label_id": "l-foreign"}}},
		{"no label store", func() ExecutorDeps { return ExecutorDeps{} }, workflow.Node{ID: "n1", Type: workflow.NodeTypeActionAssignLabel, Config: map[string]interface{}{"label_id": "l-vip"}}},
		{"unknown stage", func() ExecutorDeps { return crmDeps(&crmLabels{}) }, workflow.Node{ID: "n1", Type: workflow.NodeTypeActionMoveStage, Config: map[string]interface{}{"stage_id": "st-gone"}}},
		{"unknown member", func() ExecutorDeps { return crmDeps(&crmLabels{}) }, workflow.Node{ID: "n1", Type: workflow.NodeTypeActionAssignMember, Config: map[string]interface{}{"member_id": "u-gone"}}},
		{"unknown department", func() ExecutorDeps { return crmDeps(&crmLabels{}) }, workflow.Node{ID: "n1", Type: workflow.NodeTypeActionTransferDepartment, Config: map[string]interface{}{"department_id": "d-gone"}}},
		{"no department store", func() ExecutorDeps { return ExecutorDeps{} }, workflow.Node{ID: "n1", Type: workflow.NodeTypeActionTransferDepartment, Config: map[string]interface{}{"department_id": "d-fin"}}},
		{"unknown pipeline", func() ExecutorDeps { return crmDeps(&crmLabels{}) }, workflow.Node{ID: "n1", Type: workflow.NodeTypeActionManageOpportunity, Config: map[string]interface{}{"pipeline_id": "p-gone", "action": "create", "title": "Plano"}}},
		{"no deal preview", func() ExecutorDeps { return ExecutorDeps{} }, workflow.Node{ID: "n1", Type: workflow.NodeTypeActionManageOpportunity, Config: map[string]interface{}{"pipeline_id": "p-vendas", "action": "create", "title": "Plano"}}},
		{"unknown lead field", func() ExecutorDeps { return crmDeps(&crmLabels{}) }, workflow.Node{ID: "n1", Type: workflow.NodeTypeActionUpdateLead, Config: map[string]interface{}{"custom_fields": map[string]interface{}{"turno": "noite"}}}},
		{"no lead preview", func() ExecutorDeps { return ExecutorDeps{} }, workflow.Node{ID: "n1", Type: workflow.NodeTypeActionUpdateLead, Config: map[string]interface{}{"custom_fields": map[string]interface{}{"curso": "Direito"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			box := newSandboxRun(tc.deps())
			result := box.execute(t, tc.node)
			if result.Output["success"] != false {
				t.Fatalf("output = %v, want a failure", result.Output)
			}
			if changes := box.effects.drain().Changes; len(changes) != 0 {
				t.Fatalf("a failed node must not claim a change: %+v", changes)
			}
		})
	}
}
