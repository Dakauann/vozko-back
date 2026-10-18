package crmtools

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/workspace"
	"vozko/usecases/copilot/copilottools/tooltest"
)

func TestActivateWorkflowActsAsTheUserAndReturnsTheConsentRefusalCoded(t *testing.T) {
	wfs := &fakeScopedWorkflows{err: workspace.ErrConsentGrantForbidden}
	cc := tooltest.Member()
	cc.SystemAdmin = true
	res := NewActivateWorkflowTool(wfs).Execute(context.Background(), cc, map[string]interface{}{"workflow_id": knownWorkflow})

	if res.Status != copilot.StatusDenied || !strings.Contains(res.Message, "lead_consent_grant_forbidden") {
		t.Fatalf("status %s message %q", res.Status, res.Message)
	}
	if want := (workspace.Requester{UserID: "u-1", SystemAdmin: true}); wfs.scope.Requester != want {
		t.Fatalf("requester = %+v", wfs.scope.Requester)
	}
}
