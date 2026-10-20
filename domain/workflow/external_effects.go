package workflow

import "regexp"

var fetchCall = regexp.MustCompile(`(^|[^\w$.])fetch\s*\(`)

func CodeUsesFetch(code string) bool {
	return fetchCall.MatchString(code)
}

func alwaysCallsOut(t NodeType) bool {
	switch t {
	case NodeTypeActionHTTPRequest, NodeTypeActionSendEmail,
		NodeTypeActionScheduleMeeting, NodeTypeActionRescheduleMeeting, NodeTypeActionCheckCalendarAvailability:
		return true
	}
	return false
}

func MayCallOut(t NodeType) bool {
	return alwaysCallsOut(t) || t == NodeTypeActionCode
}

func ExternalCallNode(n Node) bool {
	if alwaysCallsOut(n.Type) {
		return true
	}
	if n.Type == NodeTypeActionCode {
		code, _ := n.Config["code"].(string)
		return CodeUsesFetch(code)
	}
	return false
}

type TestBehaviors map[NodeType]NodeTestBehavior

func TestBehaviorsOf(defs []NodeDefinition) TestBehaviors {
	out := make(TestBehaviors, len(defs))
	for _, def := range defs {
		out[def.Type.Canonical()] = def.TestBehavior
	}
	return out
}

func (b NodeTestBehavior) HasSideEffect() bool {
	switch b {
	case NodeTestExternalCall, NodeTestSendsMessage, NodeTestWritesData, NodeTestAIModel, NodeTestVoiceOnly:
		return true
	}
	return false
}

func SideEffectNode(n Node, behaviors TestBehaviors) bool {
	if ExternalCallNode(n) {
		return true
	}
	declared, ok := behaviors[n.Type.Canonical()]
	return !ok || declared.HasSideEffect()
}
