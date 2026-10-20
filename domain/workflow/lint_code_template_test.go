package workflow

import (
	"strings"
	"testing"
)

func codeNodeGraph(code string) *Graph {
	return &Graph{
		Nodes: []Node{
			{ID: "n1", Type: NodeTypeTriggerWebhook},
			{ID: "n55", Type: NodeTypeActionHTTPRequest},
			{ID: "n56", Type: NodeTypeActionCode, Config: map[string]interface{}{"code": code}},
			{ID: "end", Type: NodeTypeEnd},
		},
		Edges: []Edge{{Source: "n1", Target: "n55"}, {Source: "n55", Target: "n56"}, {Source: "n56", Target: "end"}},
	}
}

func codeTemplateIssues(g *Graph) []LintIssue {
	var issues []LintIssue
	for _, i := range LintGraph(g, WorkflowTypeMessages, nil, nil, nil).Issues {
		if i.Code == LintCodeReadsTemplate {
			issues = append(issues, i)
		}
	}
	return issues
}

func TestLintWarnsWhenCodeReadsATemplateBecauseCodeIsNeverInterpolated(t *testing.T) {
	issues := codeTemplateIssues(codeNodeGraph(`const s = "{{node.n55.status_code}}"; return { s };`))
	if len(issues) != 1 {
		t.Fatalf("expected one issue, got %+v", issues)
	}
	issue := issues[0]
	if issue.NodeID != "n56" || issue.Field != "code" || issue.Severity != SeverityAdvisory {
		t.Fatalf("unexpected issue shape: %+v", issue)
	}
	if !strings.Contains(issue.Message, "n56") {
		t.Fatalf("message must name the node: %q", issue.Message)
	}
	for _, want := range []string{"input.", "state.get('node.n55.status_code')"} {
		if !strings.Contains(issue.Hint, want) {
			t.Fatalf("hint %q must suggest %q", issue.Hint, want)
		}
	}
}

func TestLintCodeTemplateRuleSuggestsAGenericPathWhenTheTemplateHasNoStatePath(t *testing.T) {
	issues := codeTemplateIssues(codeNodeGraph(`return { d: "{{sys.date}}" };`))
	if len(issues) != 1 {
		t.Fatalf("expected one issue, got %+v", issues)
	}
	if !strings.Contains(issues[0].Hint, "state.get('node.<id>.<campo>')") {
		t.Fatalf("hint must fall back to the generic path: %q", issues[0].Hint)
	}
}

func TestLintCodeTemplateRuleStaysQuietForCodeWithoutTemplates(t *testing.T) {
	cases := []struct {
		name string
		g    *Graph
	}{
		{"plain code", codeNodeGraph(`return { status: input.status_code };`)},
		{"template outside a code node", &Graph{Nodes: []Node{{ID: "t", Type: NodeTypeActionSendText, Config: map[string]interface{}{"message": "{{last.text}}"}}}}},
		{"code that is not a string", codeNodeGraph("")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if issues := codeTemplateIssues(c.g); len(issues) != 0 {
				t.Fatalf("unexpected issues: %+v", issues)
			}
		})
	}
}

func TestTheCodeTemplateIssuePointsAtTheCode(t *testing.T) {
	g := singleNodeGraph(Node{ID: "n56", Type: NodeTypeActionCode, Config: map[string]interface{}{"code": "return { s: '{{node.n55.status_code}}' };"}})
	for _, issue := range LintGraph(g, WorkflowTypeMessages, nil, nil, nil).Issues {
		if issue.Code == LintCodeReadsTemplate {
			if issue.Path != "code" {
				t.Fatalf("path = %q", issue.Path)
			}
			return
		}
	}
	t.Fatal("expected the code template issue")
}
