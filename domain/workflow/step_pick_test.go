package workflow

import (
	"reflect"
	"slices"
	"testing"
)

func TestAReproductionReadsOnlyTheStepsOfTheNodesItReplays(t *testing.T) {
	draft := reproductionDraft()
	behaviors := TestBehaviorsOf([]NodeDefinition{
		{Type: NodeTypeActionHTTPRequest, TestBehavior: NodeTestExternalCall},
		{Type: NodeTypeActionCode, TestBehavior: NodeTestExternalCall},
		{Type: NodeTypeActionSendText, TestBehavior: NodeTestSendsMessage},
		{Type: NodeTypeActionSetVariable, TestBehavior: NodeTestPure},
	})
	pick := ReproductionPick(&draft, behaviors)
	if want := []string{"calc", "login", "new", "send"}; !reflect.DeepEqual(pick.LatestOfNodes, want) {
		t.Fatalf("nodes = %v, want %v", pick.LatestOfNodes, want)
	}
	if !pick.Trigger || pick.StepID != "" {
		t.Fatalf("pick = %+v, a reproduction also needs the trigger step when the run kept no trigger data", pick)
	}
}

func TestAPinReadsTheChosenStepOrTheLatestUsableOneOfItsNode(t *testing.T) {
	if got := PinPick("login", "step-7"); got.StepID != "step-7" || len(got.LatestOfNodes) != 0 || got.Trigger {
		t.Fatalf("chosen step pick = %+v", got)
	}
	if got := PinPick("login", ""); got.StepID != "" || !reflect.DeepEqual(got.LatestOfNodes, []string{"login"}) || got.Trigger {
		t.Fatalf("latest step pick = %+v", got)
	}
}

func TestOnlyExecutedOrSkippedStepsCanBePinned(t *testing.T) {
	for _, status := range []LogStatus{LogStatusExecuted, LogStatusSkipped, LogStatusFailed, LogStatus("waiting")} {
		step := &WorkflowRunLog{Status: status}
		if step.Pinnable() != slices.Contains(PinnableStepStatuses(), status) {
			t.Fatalf("%s: Pinnable() and the status list a query filters on disagree", status)
		}
	}
}

func TestEveryListedTriggerNodeTypeIsATrigger(t *testing.T) {
	for _, nodeType := range TriggerNodeTypes() {
		if !nodeType.IsTrigger() {
			t.Fatalf("%s is listed as a trigger but IsTrigger says no", nodeType)
		}
	}
	if NodeTypeActionSendText.IsTrigger() || NodeTypeEnd.IsTrigger() {
		t.Fatal("an action is not a trigger")
	}
}
