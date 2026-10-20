package buttonaction

import (
	"testing"

	"vozko/domain/conversation"
)

func TestADelegateActionNamesTheAutomationThatTakesTheConversation(t *testing.T) {
	if got := (Params{AgentID: "agent-1"}).Automation(); got != (conversation.Automation{Kind: conversation.AutomationAgent, ID: "agent-1"}) {
		t.Fatalf("agent = %+v", got)
	}
	if got := (Params{WorkflowID: "wf-1"}).Automation(); got != (conversation.Automation{Kind: conversation.AutomationWorkflow, ID: "wf-1"}) {
		t.Fatalf("workflow = %+v", got)
	}
}
