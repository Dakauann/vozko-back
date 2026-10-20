package workflow

import "testing"

func TestLintTellsEveryPinnedNodeIsTestOnlyWithoutBlocking(t *testing.T) {
	g := singleNodeGraph(Node{ID: "n1", Type: NodeTypeActionSetVariable, Pinned: &NodePin{Output: map[string]interface{}{}, Source: PinSourceManual, PinnedAt: pinTime}})
	var pinned []LintIssue
	report := LintGraph(g, WorkflowTypeMessages, nil, nil, nil)
	for _, i := range report.Issues {
		if i.Code == LintPinnedOutput {
			pinned = append(pinned, i)
		}
	}
	if len(pinned) != 1 {
		t.Fatalf("got %d pinned issues, want one per pinned node", len(pinned))
	}
	issue := pinned[0]
	if issue.Severity != SeverityInfo || issue.NodeID != "n1" || issue.Message != "Saída fixada (apenas testes)" {
		t.Fatalf("issue = %+v", issue)
	}
	for _, b := range report.Blocking() {
		if b.Code == LintPinnedOutput {
			t.Fatal("a pin must never block publishing, publish strips it")
		}
	}
	for _, a := range report.Advisory() {
		if a.Code == LintPinnedOutput {
			t.Fatal("a pin is information, not a warning")
		}
	}
}
