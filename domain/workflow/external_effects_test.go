package workflow

import "testing"

func TestCodeUsesFetchSpotsCallsNotWords(t *testing.T) {
	cases := []struct {
		code string
		want bool
	}{
		{"const r = fetch('https://x')", true},
		{"return await fetch ({url: u})", true},
		{"const fetched = input.fetchCount", false},
		{"return { prefetch: 1 }", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := CodeUsesFetch(tc.code); got != tc.want {
			t.Errorf("CodeUsesFetch(%q) = %v, want %v", tc.code, got, tc.want)
		}
	}
}

func TestExternalCallNodeNamesTheNodesThatReachOutsideOnTheirOwn(t *testing.T) {
	cases := []struct {
		name string
		node Node
		want bool
	}{
		{"http request", Node{Type: NodeTypeActionHTTPRequest}, true},
		{"code with fetch", Node{Type: NodeTypeActionCode, Config: map[string]interface{}{"code": "return fetch('https://x')"}}, true},
		{"code without fetch", Node{Type: NodeTypeActionCode, Config: map[string]interface{}{"code": "return {a: 1}"}}, false},
		{"email", Node{Type: NodeTypeActionSendEmail}, true},
		{"schedule meeting", Node{Type: NodeTypeActionScheduleMeeting}, true},
		{"reschedule meeting", Node{Type: NodeTypeActionRescheduleMeeting}, true},
		{"calendar availability", Node{Type: NodeTypeActionCheckCalendarAvailability}, true},
		{"whatsapp text goes through the channel sender", Node{Type: NodeTypeActionSendText}, false},
		{"set variable", Node{Type: NodeTypeActionSetVariable}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExternalCallNode(tc.node); got != tc.want {
				t.Fatalf("ExternalCallNode = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSideEffectNodeFollowsTheDeclaredTestBehaviour(t *testing.T) {
	behaviors := TestBehaviorsOf([]NodeDefinition{
		{Type: NodeTypeActionHTTPRequest, TestBehavior: NodeTestExternalCall},
		{Type: NodeTypeActionSendText, TestBehavior: NodeTestSendsMessage},
		{Type: NodeTypeActionAssignLabel, TestBehavior: NodeTestWritesData},
		{Type: NodeTypeActionAIAgent, TestBehavior: NodeTestAIModel},
		{Type: NodeTypeActionPlayAudio, TestBehavior: NodeTestVoiceOnly},
		{Type: NodeTypeActionSetVariable, TestBehavior: NodeTestPure},
		{Type: NodeTypeConditionCheckLabel, TestBehavior: NodeTestReadsData},
		{Type: NodeTypeWaitForReply, TestBehavior: NodeTestWaits},
	})
	cases := []struct {
		node NodeType
		want bool
	}{
		{NodeTypeActionHTTPRequest, true},
		{NodeTypeActionSendText, true},
		{NodeTypeActionAssignLabel, true},
		{NodeTypeActionAIAgent, true},
		{NodeTypeActionPlayAudio, true},
		{NodeTypeActionSetVariable, false},
		{NodeTypeConditionCheckLabel, false},
		{NodeTypeWaitForReply, false},
		{NodeTypeActionMoveStage, true},
	}
	for _, tc := range cases {
		if got := SideEffectNode(Node{Type: tc.node}, behaviors); got != tc.want {
			t.Errorf("SideEffectNode(%s) = %v, want %v (a type without a declared behaviour counts as a side effect)", tc.node, got, tc.want)
		}
	}
}
