package workflow

import "testing"

func TestCalendarLintRejectsInvalidLiteralDatesButAllowsWholeDayAvailability(t *testing.T) {
	for _, tc := range []struct {
		kind    NodeType
		config  map[string]interface{}
		invalid bool
	}{
		{NodeTypeActionCheckCalendarAvailability, map[string]interface{}{"date_from": "2026-10-23", "date_to": "2026-10-23"}, false},
		{NodeTypeActionCheckCalendarAvailability, map[string]interface{}{"date_from": "amanhã"}, true},
		{NodeTypeActionCheckCalendarAvailability, map[string]interface{}{"date_from": "2026-10-23T09:00:00", "date_to": "2026-10-23T09:00:00"}, true},
		{NodeTypeActionCheckCalendarAvailability, map[string]interface{}{"date_from": "{{node.agent.data}}", "date_to": "{{node.agent.data}}"}, false},
		{NodeTypeActionCreateCalendarEvent, map[string]interface{}{"start_time": "2026-10-23"}, true},
		{NodeTypeActionCreateCalendarEvent, map[string]interface{}{"start_time": "2026-10-23T09:00:00"}, false},
		{NodeTypeActionCheckCalendarAvailability, map[string]interface{}{"date_from": "2026-10-23", "timezone": "invalid"}, true},
	} {
		g := &Graph{Nodes: []Node{{ID: "calendar", Type: tc.kind, Config: tc.config}}}
		count := 0
		lintCalendarConfig(g, func(issue LintIssue) {
			if issue.Severity == SeverityBlocking {
				count++
			}
		})
		if (count > 0) != tc.invalid {
			t.Fatalf("config %+v: blockers=%d", tc.config, count)
		}
	}
}

func TestDeclaredAgentToolArgumentsAreRecognizedAsNodeOutputs(t *testing.T) {
	agent := Node{ID: "agent", Type: NodeTypeActionAIAgent, Config: map[string]interface{}{"custom_tools": []interface{}{map[string]interface{}{"name": "book", "parameters": []interface{}{map[string]interface{}{"name": "data"}}}}}}
	g := &Graph{Nodes: []Node{agent, {ID: "calendar", Type: NodeTypeActionCheckCalendarAvailability, Config: map[string]interface{}{"date_from": "{{node.agent.data}}", "date_to": "{{node.agent.typo}}"}}}, Edges: []Edge{{Source: "agent", Target: "calendar", Label: "book"}}}
	issues := []LintIssue{}
	lintDataFlow(g, map[NodeType]NodeDefinition{NodeTypeActionAIAgent: {OutputKeys: []OutputKeyDefinition{{Key: "tool_args"}}}}, func(issue LintIssue) { issues = append(issues, issue) })
	if len(issues) != 1 || issues[0].Code != LintUnknownOutputKey {
		t.Fatalf("expected only the undeclared typo warning: %+v", issues)
	}
}
