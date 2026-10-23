package workflow

import "testing"

func dataRefIssues(g *Graph) []LintIssue {
	var issues []LintIssue
	lintDataRefAvailability(g, NodeCatalogMap(catalogDefs()), func(i LintIssue) {
		if i.Code == LintDataRefNeverSet || i.Code == LintDataRefSomePaths {
			issues = append(issues, i)
		}
	})
	return issues
}

func TestAReferenceNothingCanProvideBlocksPublishing(t *testing.T) {
	g := triggerGraph(agentCallTestTrigger(), sendText("s", "Oi {{input.cpf}}, fone {{input.telefone}}"))

	issues := dataRefIssues(g)

	if len(issues) != 1 {
		t.Fatalf("issues = %+v", issues)
	}
	want := LintIssue{Code: LintDataRefNeverSet, Severity: SeverityBlocking, NodeID: "s", Field: "text", Path: "input.telefone"}
	got := issues[0]
	if got.Code != want.Code || got.Severity != want.Severity || got.NodeID != want.NodeID || got.Field != want.Field || got.Path != want.Path || got.Message == "" || got.Hint == "" {
		t.Fatalf("issue = %+v, want %+v", got, want)
	}
}

func TestAReferenceSetOnlyOnSomePathsIsAWarning(t *testing.T) {
	g := enrollmentGraph()
	g.Nodes[4].Config["text"] = "Status {{status}} {{var.cadastro.nome}} {{input.endereco.cep}}"

	issues := dataRefIssues(g)

	if len(issues) != 1 || issues[0].Code != LintDataRefSomePaths || issues[0].Severity != SeverityAdvisory || issues[0].Path != "status" || issues[0].NodeID != "s" {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestDynamicOutputsAndSpecialScopesAreNeverFlagged(t *testing.T) {
	g := enrollmentGraph()
	g.Nodes[4].Config["text"] = "{{last.bairro}} {{node.h.qualquer}} {{secret.api}} {{sys.date}} {{1}} {{_wait_outcome}} {{agent.name}}"
	if issues := dataRefIssues(g); len(issues) != 0 {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestAnUnknownSystemValueIsNeverSet(t *testing.T) {
	g := triggerGraph(agentCallTestTrigger(), sendText("s", "{{sys.amanha}}"))
	if issues := dataRefIssues(g); len(issues) != 1 || issues[0].Code != LintDataRefNeverSet {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestTheScriptOfACodeNodeIsNotReadAsTemplates(t *testing.T) {
	g := triggerGraph(agentCallTestTrigger(), Node{ID: "js", Type: NodeTypeActionCode, Config: map[string]interface{}{"code": "return '{{nada}}'"}})
	if issues := dataRefIssues(g); len(issues) != 0 {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestACodeNodeWritingComputedKeysMayProvideAnyVariable(t *testing.T) {
	g := triggerGraph(agentCallTestTrigger(),
		Node{ID: "js", Type: NodeTypeActionCode, Config: map[string]interface{}{"code": "state.set(nome, 1)"}},
		sendText("s", "{{qualquer}}"))
	if issues := dataRefIssues(g); len(issues) != 0 {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestAFormTriggerWhoseFormsAreUnknownToTheLintNeverBlocksItsAnswers(t *testing.T) {
	g := triggerGraph(Node{ID: "f", Type: NodeTypeTriggerFormSubmitted, Config: map[string]interface{}{FormTriggerFormIDsKey: []interface{}{"form-1"}}}, sendText("s", "{{form.nome}} {{form_id}}"))
	if issues := dataRefIssues(g); len(issues) != 0 {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestANodeNoTriggerReachesIsLeftToTheOrphanLint(t *testing.T) {
	g := triggerGraph(agentCallTestTrigger(), sendText("s", "ok"))
	g.Nodes = append(g.Nodes, sendText("solto", "{{nada}}"))
	if issues := dataRefIssues(g); len(issues) != 0 {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestLintGraphReportsTheNeverSetReference(t *testing.T) {
	g := triggerGraph(agentCallTestTrigger(), sendText("s", "{{input.telefone}}"))
	report := LintGraph(g, WorkflowTypeMessages, catalogDefs(), nil, nil)
	for _, issue := range report.Blocking() {
		if issue.Code == LintDataRefNeverSet && issue.Path == "input.telefone" {
			return
		}
	}
	t.Fatalf("blocking = %+v", report.Blocking())
}

func TestHiddenOutputsAndTheLastStepAliasStillResolveInTemplates(t *testing.T) {
	g := enrollmentGraph()
	g.Nodes[4].Config["text"] = "{{last.status_code}} {{node.c.operator}} {{last.matched}}"
	if issues := dataRefIssues(g); len(issues) != 0 {
		t.Fatalf("issues = %+v", issues)
	}
}
