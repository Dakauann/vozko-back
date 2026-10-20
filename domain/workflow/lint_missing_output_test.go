package workflow

import "testing"

func missingOutputIssues(report LintReport) []LintIssue {
	var issues []LintIssue
	for _, i := range report.Issues {
		if i.Code == LintMissingRequiredOutput {
			issues = append(issues, i)
		}
	}
	return issues
}

func branchingGraph() *Graph {
	return &Graph{
		Nodes: []Node{{ID: "t", Type: NodeTypeTriggerWebhook}, {ID: "pick", Type: "action_pick"}, {ID: "end", Type: NodeTypeEnd}},
		Edges: []Edge{{Source: "t", Target: "pick"}, {Source: "pick", Target: "end", Label: "sucesso"}},
	}
}

func TestAMissingRequiredOutputIssueNamesTheOutputInItsPath(t *testing.T) {
	catalogOutputs := []NodeDefinition{{Type: "action_pick", Category: NodeCategoryAction, Scopes: []NodeScope{NodeScopeShared}, Outputs: []HandleDefinition{{ID: "sucesso", Label: "Sucesso"}, {ID: "erro", Label: "Erro"}}}}
	dynamicOutputs := func(n Node) ([]HandleDefinition, bool) {
		if n.Type != "action_pick" {
			return nil, false
		}
		return []HandleDefinition{{ID: "sucesso", Label: "Sucesso"}, {ID: "no_match", Label: "Sem correspondência"}, {ID: "no_reply", Label: "Sem resposta", Optional: true}}, true
	}
	tests := []struct {
		name     string
		catalog  []NodeDefinition
		resolver DynamicHandleResolver
		want     []string
	}{
		{name: "an output declared by the catalog", catalog: catalogOutputs, want: []string{"erro"}},
		{name: "an output resolved from the node config, optional ones never reported", resolver: dynamicOutputs, want: []string{"no_match"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issues := missingOutputIssues(LintGraph(branchingGraph(), WorkflowTypeMessages, tt.catalog, nil, tt.resolver))
			if len(issues) != len(tt.want) {
				t.Fatalf("issues = %+v, want one per unconnected required output %v", issues, tt.want)
			}
			for i, issue := range issues {
				if issue.NodeID != "pick" || issue.Path != tt.want[i] || issue.Severity != SeverityBlocking {
					t.Errorf("issue = %+v, want node pick with path %q so the editor can name the output", issue, tt.want[i])
				}
			}
		})
	}
}

func TestAMissingOutputErrorKeepsItsSentinelAndText(t *testing.T) {
	problems := RequiredOutputProblems(branchingGraph(), []NodeDefinition{{Type: "action_pick", Outputs: []HandleDefinition{{ID: "erro"}}}})
	if len(problems) != 1 {
		t.Fatalf("problems = %v", problems)
	}
	if !IsValidationError(problems[0]) {
		t.Fatal("a missing output must stay a validation error")
	}
	if got, want := problems[0].Error(), `workflow: node is missing a required output edge: node "pick" output "erro"`; got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
	if issue := IssueOf(problems[0]); issue.Path != "erro" || issue.NodeID != "pick" {
		t.Fatalf("IssueOf = %+v, want the output id in its path", issue)
	}
}
