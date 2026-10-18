package platformtools

import (
	"context"
	"strings"
	"testing"

	"vozko/domain/agent"
	"vozko/domain/copilot"
	"vozko/domain/workspace"
)

type consentRefusingCreate struct{ got agent.CreateAgentInput }

func (f *consentRefusingCreate) Execute(_ context.Context, in agent.CreateAgentInput) (*agent.Agent, error) {
	f.got = in
	return nil, workspace.ErrConsentGrantForbidden
}

type consentRefusingUpdate struct{ got agent.UpdateAgentInput }

func (f *consentRefusingUpdate) Execute(_ context.Context, _ string, in agent.UpdateAgentInput) (*agent.Agent, error) {
	f.got = in
	return nil, workspace.ErrConsentGrantForbidden
}

func grantorContext() copilot.Context {
	cc := okContext()
	cc.UserID, cc.SystemAdmin = "u-7", true
	return cc
}

func assertCodedConsentRefusal(t *testing.T, res copilot.Result) {
	t.Helper()
	if res.Status != copilot.StatusDenied || !strings.Contains(res.Message, "lead_consent_grant_forbidden") {
		t.Fatalf("status %s message %q", res.Status, res.Message)
	}
}

func TestEloAgentToolsSaveAsTheUserAndReturnTheConsentRefusalCoded(t *testing.T) {
	want := workspace.Requester{UserID: "u-7", SystemAdmin: true}
	consentTool := []interface{}{map[string]interface{}{"name": "record_consent", "config": map[string]interface{}{"purpose": "ofertas"}}}

	create := &consentRefusingCreate{}
	res := NewCreateAgentTool(create, AgentDeps{}).Execute(context.Background(), grantorContext(), map[string]interface{}{
		"name": "Ana", "messagingPrompt": "p", "messagingModel": "openai/gpt-4o", "provider": "platform", "internalTools": consentTool,
	})
	assertCodedConsentRefusal(t, res)
	if create.got.RequestedBy != want {
		t.Fatalf("create requester = %+v", create.got.RequestedBy)
	}

	update := &consentRefusingUpdate{}
	res = NewUpdateAgentTool(fakeGetAgent{a: boundAgent()}, update, AgentDeps{}).Execute(context.Background(), grantorContext(), map[string]interface{}{
		"id": "ag-1", "addTools": consentTool,
	})
	assertCodedConsentRefusal(t, res)
	if update.got.RequestedBy != want {
		t.Fatalf("update requester = %+v", update.got.RequestedBy)
	}
}
