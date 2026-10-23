package emailsending

import "testing"

func TestOnlyAPISendsKeepRepliesOutOfTheInbox(t *testing.T) {
	for kind, inbox := range map[RunKind]bool{RunCampaign: true, RunTriggered: true, RunReply: true, RunTest: true, RunAPI: false} {
		if kind.RoutesRepliesToInbox() != inbox {
			t.Fatalf("%s routes replies to the inbox = %v, want %v", kind, !inbox, inbox)
		}
	}
}

func TestAPISendsArePrioritySends(t *testing.T) {
	if SendLaneOf(RunAPI) != SendLanePriority {
		t.Fatal("an API send must take the priority lane like the other single sends")
	}
}
