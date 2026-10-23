package node_executors

import (
	"testing"

	"vozko/domain/workflow"
)

func TestNodesThatNeedAConversationSayIt(t *testing.T) {
	needs := map[string]workflow.NodeDefiner{
		"send text":           NewSendTextExecutor(SenderDeps{}),
		"send media":          NewSendMediaExecutor(SenderDeps{}),
		"send form":           NewSendFormExecutor(nil, SenderDeps{}),
		"send template":       NewSendTemplateExecutor(SenderDeps{}),
		"send interactive":    NewInteractivePromptExecutor(SenderDeps{}),
		"wait for reply":      &waitForReplyExecutor{},
		"finish conversation": &finishConversationExecutor{},
		"assign member":       &assignMemberExecutor{},
		"transfer department": &transferDepartmentExecutor{},
		"ai agent":            &aiAgentExecutor{},
	}
	for name, node := range needs {
		if !node.Definition().NeedsConversation {
			t.Errorf("%s needs a conversation", name)
		}
	}
	works := map[string]workflow.NodeDefiner{
		"send carrier":        NewSendCarrierExecutor(CarrierNodeDeps{}),
		"send platform email": NewSendPlatformEmailExecutor(PlatformEmailNodeDeps{}),
	}
	for name, node := range works {
		if node.Definition().NeedsConversation {
			t.Errorf("%s works on a lead run", name)
		}
	}
}
