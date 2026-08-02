package workflow

import "testing"

// A run parks on two kinds of node: an explicit wait, and an interactive prompt
// that has just offered the contact a choice. Five places in the engine branch
// on "did we just resume from one of those?" and the interactive prompt is
// deliberately NOT part of IsWait, so each one had to remember to spell out the
// second clause. Two of them didn't.
//
// The one that mattered was the AI agent's stale-tool-result guard: after a
// button press it replayed the previous tool's output as a USER message, so the
// model read its own last action as a fresh request and called the same tool
// again. Every option led back to the same menu, which looks exactly like the
// press being ignored.

func TestParksForReplyCoversBothWaitingNodes(t *testing.T) {
	if !NodeTypeActionSendInteractive.ParksForReply() {
		t.Error("an interactive prompt parks until the contact chooses")
	}
	// The legacy wire value must behave identically, or a workflow saved before
	// the rename loses its branching.
	if !NodeTypeActionSendWhatsappButtonLegacy.ParksForReply() {
		t.Error("a legacy interactive prompt parks too")
	}
	if !NodeTypeWaitForReply.ParksForReply() {
		t.Error("a wait-for-reply node parks")
	}
}

// The interactive prompt stays out of IsWait on purpose — IsWait also drives the
// node catalog's "wait" category. This pins the split so nobody "simplifies" it
// and silently changes which category the node appears in.
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
