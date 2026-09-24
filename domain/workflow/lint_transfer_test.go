package workflow

import "testing"

func TestLintAcceptsATransferWithoutADepartment(t *testing.T) {
	// An empty department means "the conversation's own": a valid, working
	// step, not a missing reference.
	g := &Graph{Nodes: []Node{{ID: "n1", Type: NodeTypeActionTransferDepartment, Config: map[string]interface{}{"department_id": ""}}}}
	var issues []LintIssue
	lintFunctionalResourceRefs(g, func(i LintIssue) { issues = append(issues, i) })

	for _, i := range issues {
		if i.NodeID == "n1" && i.Field == "department_id" {
			t.Fatalf("unexpected issue: %+v", i)
		}
	}
}
