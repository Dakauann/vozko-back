package workflow

import "testing"

func TestAnEngineRetryIsTheSameNodeVisitAndALoopBackIsANewOne(t *testing.T) {
	run := &WorkflowRun{CurrentNodeID: "send", State: NewRunState()}
	at := func(step, retry int) string {
		run.State.Set(StateKeyDurableSteps, step)
		run.RetryCount = retry
		return run.NodeVisit()
	}

	first := at(3, 0)
	if at(4, 1) != first || at(5, 2) != first {
		t.Fatal("an engine retry must stay on the same visit")
	}
	if at(9, 0) == first {
		t.Fatal("a later pass through the node must be a new visit")
	}
	run.CurrentNodeID = "other"
	if at(3, 0) == first {
		t.Fatal("another node is another visit")
	}
}
