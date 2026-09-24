package workflow

import "testing"

func TestLintFlagsAStageNodeWithoutAStage(t *testing.T) {
	for _, nodeType := range []NodeType{NodeTypeActionMoveStage, NodeTypeConditionCheckStage} {
		g := &Graph{Nodes: []Node{{ID: "n1", Type: nodeType, Config: map[string]interface{}{"stage_id": ""}}}}
		var issues []LintIssue
		lintFunctionalResourceRefs(g, func(i LintIssue) { issues = append(issues, i) })

		found := false
		for _, i := range issues {
			if i.NodeID == "n1" && i.Field == "stage_id" {
				found = true
			}
		}
		if !found {
			t.Errorf("%s without a stage was not flagged: %+v", nodeType, issues)
		}
	}
}
