package formtools

import (
	"context"
	"strings"
	"testing"
	"time"

	"vozko/domain/copilot"
	"vozko/usecases/copilot/copilottools/tooltest"
	workflow_usecase "vozko/usecases/workflow"
)

func TestCreateFormAutomationScaffoldsTheWorkflowOfTheForm(t *testing.T) {
	forms := formsWith(knownDetail())
	forms.workflowID = "wf-1"
	tool := NewCreateFormAutomationTool(deps(forms))

	if err := tooltest.Validate(t, tool, tooltest.Member(), map[string]interface{}{"form_id": knownForm}); err != nil {
		t.Fatalf("validate: %v", err)
	}
	res := tool.Execute(context.Background(), tooltest.Member(), map[string]interface{}{"form_id": knownForm})

	if res.Status != copilot.StatusOK || !forms.called("ScaffoldAutomation:"+knownForm) || tooltest.DataOf(res)["workflow_id"] != "wf-1" {
		t.Fatalf("res = %+v, calls %v", res, forms.calls)
	}
	if !strings.Contains(tooltest.DataOf(res)["next"].(string), "rascunho") {
		t.Fatalf("data = %+v", tooltest.DataOf(res))
	}
}

func TestCreateFormAutomationRefusesAnArchivedForm(t *testing.T) {
	archived := knownDetail()
	now := time.Now()
	archived.Form.ArchivedAt = &now

	err := tooltest.Validate(t, NewCreateFormAutomationTool(deps(formsWith(archived))), tooltest.Member(), map[string]interface{}{"form_id": knownForm})

	if err == nil || !strings.Contains(err.Error(), "arquivado") {
		t.Fatalf("err = %v", err)
	}
}

func TestCreateFormAutomationCardNamesTheForm(t *testing.T) {
	fields := NewCreateFormAutomationTool(deps(formsWith(knownDetail()))).(copilot.Describer).Describe(context.Background(), tooltest.Member(), map[string]interface{}{"form_id": knownForm})

	if tooltest.FieldValue(fields, "form") != "Captação" || !strings.Contains(tooltest.FieldValue(fields, "workflow"), "Captação") {
		t.Fatalf("fields = %+v", fields)
	}
}

func TestCreateFormAutomationExplainsAMissingDealFunnel(t *testing.T) {
	forms := formsWith(knownDetail())
	forms.errOn = map[string]error{"ScaffoldAutomation:" + knownForm: workflow_usecase.ErrNoDefaultDealFunnel}

	res := NewCreateFormAutomationTool(deps(forms)).Execute(context.Background(), tooltest.Member(), map[string]interface{}{"form_id": knownForm})

	if res.Status == copilot.StatusOK || !strings.Contains(res.Message, "funil") {
		t.Fatalf("res = %+v", res)
	}
}

func TestCreateFormAutomationNeedsToCreateWorkflows(t *testing.T) {
	tool := NewCreateFormAutomationTool(deps(&fakeForms{}))
	if m := tool.Meta(); !m.Mutating || m.Resource != "workflows" || m.Action != "create" {
		t.Fatalf("meta = %+v", m)
	}
	if !requires(tool, "whatsapp_forms:read", "workflows:read", "workflows:create") {
		t.Fatalf("requires %v", requirementKeys(tool))
	}
}
