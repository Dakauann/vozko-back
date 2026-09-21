package workflow

import "testing"

func TestParksForReplyCoversBothWaitingNodes(t *testing.T) {
	if !NodeTypeActionSendInteractive.ParksForReply() {
		t.Error("an interactive prompt parks until the contact chooses")
	}
	if !NodeTypeActionSendWhatsappButtonLegacy.ParksForReply() {
		t.Error("a legacy interactive prompt parks too")
	}
	if !NodeTypeWaitForReply.ParksForReply() {
		t.Error("a wait-for-reply node parks")
	}
}

func TestTheInteractivePromptIsStillNotAWaitNode(t *testing.T) {
	if NodeTypeActionSendInteractive.IsWait() {
		t.Error("folding it into IsWait would move it in the node catalog")
	}
}

func TestParksForReplyExcludesOrdinaryNodes(t *testing.T) {
	for _, nt := range []NodeType{
		NodeTypeActionSendText,
		NodeTypeActionSendMedia,
		NodeTypeActionAIAgent,
		NodeTypeTriggerMessageReceived,
	} {
		if nt.ParksForReply() {
			t.Errorf("%s does not park the run", nt)
		}
	}
}
