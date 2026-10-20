package whatsappform_usecase

import (
	"context"
	"errors"
	"testing"

	wf "vozko/domain/whatsappform"
	"vozko/domain/workspace"
)

type automationCatalog struct {
	listed    []FormAutomation
	scaffolds []string
}

func (c *automationCatalog) List(_ context.Context, workspaceID, formID string) ([]FormAutomation, error) {
	return c.listed, nil
}

func (c *automationCatalog) Scaffold(_ context.Context, workspaceID, userID, formID, formName string) (string, error) {
	c.scaffolds = append(c.scaffolds, workspaceID+"/"+userID+"/"+formID+"/"+formName)
	return "wf-1", nil
}

func TestAutomationsOfAForm(t *testing.T) {
	w := newWorld(t, wf.CapabilityView, wf.CapabilityBuild, workspace.CapabilityKey("workflows.create"))
	detail := w.create("Cadastro", "")
	w.automations.listed = []FormAutomation{{WorkflowID: "wf-9", Name: "Boas-vindas", Use: AutomationTrigger}}
	listed, err := w.svc.Automations(ctx, w.viewer, detail.Form.ID)
	if err != nil || len(listed) != 1 || listed[0].WorkflowID != "wf-9" {
		t.Fatalf("listed = %+v %v", listed, err)
	}
	id, err := w.svc.ScaffoldAutomation(ctx, w.viewer, detail.Form.ID)
	if err != nil || id != "wf-1" || w.automations.scaffolds[0] != ws+"/"+member+"/"+detail.Form.ID+"/Cadastro" {
		t.Fatalf("scaffold = %q %v %v", id, err, w.automations.scaffolds)
	}
}

func TestScaffoldingAnAutomationNeedsTheRightToCreateWorkflows(t *testing.T) {
	w := builder(t)
	detail := w.create("Cadastro", "")
	if _, err := w.svc.ScaffoldAutomation(ctx, w.viewer, detail.Form.ID); !errors.Is(err, wf.ErrForbidden) {
		t.Fatalf("err = %v", err)
	}
}
