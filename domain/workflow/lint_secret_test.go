package workflow

import (
	"strings"
	"testing"
)

func singleNodeGraph(n Node) *Graph {
	return &Graph{
		Nodes: []Node{{ID: "t", Type: NodeTypeTriggerWebhook}, n, {ID: "end", Type: NodeTypeEnd}},
		Edges: []Edge{{Source: "t", Target: n.ID}, {Source: n.ID, Target: "end"}},
	}
}

func plainSecretIssues(g *Graph) []LintIssue {
	var issues []LintIssue
	for _, i := range LintGraph(g, WorkflowTypeMessages, nil, nil, nil).Issues {
		if i.Code == LintSecretInPlainText {
			issues = append(issues, i)
		}
	}
	return issues
}

func httpNode(config map[string]interface{}) Node {
	return Node{ID: "n55", Type: NodeTypeActionHTTPRequest, Config: config}
}

func TestLintWarnsAboutCredentialsTypedIntoTheGraph(t *testing.T) {
	cases := []struct {
		name      string
		node      Node
		field     string
		suggested string
	}{
		{"authorization header", httpNode(map[string]interface{}{"headers": map[string]interface{}{"Authorization": "Bearer abc123"}}), "headers", "{{secret.authorization}}"},
		{"api key header", httpNode(map[string]interface{}{"headers": map[string]interface{}{"X-Api-Key": "k-1"}}), "headers", "{{secret.x_api_key}}"},
		{"webhook token header as json text", httpNode(map[string]interface{}{"headers": `{"X-Webhook-Token":"tok"}`}), "headers", "{{secret.x_webhook_token}}"},
		{"auth token field", httpNode(map[string]interface{}{"auth_type": "bearer", "auth_token": "abc"}), "auth_token", "{{secret.auth_token}}"},
		{"password in a json body", httpNode(map[string]interface{}{"body": `{"login":"ana","password":"p4ss"}`}), "body", "{{secret.password}}"},
		{"set variable password", Node{ID: "n9", Type: NodeTypeActionSetVariable, Config: map[string]interface{}{"variable": "unifecaf_senha", "value": "p4ss"}}, "value", "{{secret.unifecaf_senha}}"},
		{"set variable numeric secret", Node{ID: "n9", Type: NodeTypeActionSetVariable, Config: map[string]interface{}{"variable": "api_key", "value": 1234}}, "value", "{{secret.api_key}}"},
		{"code assigns a password", Node{ID: "n56", Type: NodeTypeActionCode, Config: map[string]interface{}{"code": "const senha = 'p4ss';\nreturn {};"}}, "code", "secrets.get('senha')"},
		{"code object literal token", Node{ID: "n56", Type: NodeTypeActionCode, Config: map[string]interface{}{"code": `return { "accessToken": "abc" };`}}, "code", "secrets.get('accesstoken')"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			issues := plainSecretIssues(singleNodeGraph(c.node))
			if len(issues) != 1 {
				t.Fatalf("expected one issue, got %+v", issues)
			}
			issue := issues[0]
			if issue.NodeID != c.node.ID || issue.Field != c.field || issue.Severity != SeverityAdvisory {
				t.Fatalf("unexpected shape: %+v", issue)
			}
			if !strings.HasPrefix(issue.Message, "Segredo em texto no fluxo") || !strings.Contains(issue.Message, c.node.ID) {
				t.Fatalf("message must open with the rule name and name the node: %q", issue.Message)
			}
			if !strings.Contains(issue.Hint, "segredos do workspace") || !strings.Contains(issue.Hint, c.suggested) {
				t.Fatalf("hint %q must point to the workspace secrets and suggest %q", issue.Hint, c.suggested)
			}
		})
	}
}

func TestLintStaysQuietWhenCredentialsComeFromTemplatesOrSecrets(t *testing.T) {
	cases := []struct {
		name string
		node Node
	}{
		{"header from a secret", httpNode(map[string]interface{}{"headers": map[string]interface{}{"Authorization": "Bearer {{secret.unifecaf}}"}})},
		{"header from a previous node", httpNode(map[string]interface{}{"headers": map[string]interface{}{"Authorization": "Bearer {{node.n55.accessToken}}"}})},
		{"ordinary header", httpNode(map[string]interface{}{"headers": map[string]interface{}{"Content-Type": "application/json"}})},
		{"auth token from a secret", httpNode(map[string]interface{}{"auth_token": "{{secret.k}}"})},
		{"body password from a secret", httpNode(map[string]interface{}{"body": `{"password":"{{secret.senha}}"}`})},
		{"set variable from a template", Node{ID: "n9", Type: NodeTypeActionSetVariable, Config: map[string]interface{}{"variable": "token", "value": "{{last.accessToken}}"}}},
		{"set variable with an ordinary name", Node{ID: "n9", Type: NodeTypeActionSetVariable, Config: map[string]interface{}{"variable": "curso", "value": "ADS"}}},
		{"code reads a secret", Node{ID: "n56", Type: NodeTypeActionCode, Config: map[string]interface{}{"code": "const senha = secrets.get('senha');\nif (senha === 'x') {}\nreturn {};"}}},
		{"code compares a token", Node{ID: "n56", Type: NodeTypeActionCode, Config: map[string]interface{}{"code": "if (input.token == 'abc') { return {}; }"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if issues := plainSecretIssues(singleNodeGraph(c.node)); len(issues) != 0 {
				t.Fatalf("unexpected issues: %+v", issues)
			}
		})
	}
}

func TestLintCodeTemplateRuleSuggestsSecretsGetForASecretPlaceholder(t *testing.T) {
	issues := codeTemplateIssues(codeNodeGraph(`const k = "{{secret.api_key}}"; return { k };`))
	if len(issues) != 1 {
		t.Fatalf("expected one issue, got %+v", issues)
	}
	if !strings.Contains(issues[0].Hint, "secrets.get('api_key')") {
		t.Fatalf("hint must suggest secrets.get: %q", issues[0].Hint)
	}
	if strings.Contains(issues[0].Hint, "state.get('secret.") {
		t.Fatalf("hint must not suggest reading a secret from state: %q", issues[0].Hint)
	}
}

func TestAPlainSecretIssueCarriesThePathOfTheLiteral(t *testing.T) {
	cases := []struct {
		name string
		node Node
		path string
	}{
		{"header", httpNode(map[string]interface{}{"headers": map[string]interface{}{"Authorization": "Bearer abc123"}}), "headers.Authorization"},
		{"header as json text", httpNode(map[string]interface{}{"headers": `{"X-Webhook-Token":"tok"}`}), "headers.X-Webhook-Token"},
		{"auth token", httpNode(map[string]interface{}{"auth_token": "abc"}), "auth_token"},
		{"top level body key", httpNode(map[string]interface{}{"body": `{"login":"ana","password":"p4ss"}`}), "body.password"},
		{"nested body key", httpNode(map[string]interface{}{"body": `{"login":{"usuario":"ana","senha":"p4ss"}}`}), "body.login.senha"},
		{"set variable", Node{ID: "n9", Type: NodeTypeActionSetVariable, Config: map[string]interface{}{"variable": "unifecaf_senha", "value": "p4ss"}}, "config.value"},
		{"code", Node{ID: "n56", Type: NodeTypeActionCode, Config: map[string]interface{}{"code": "const senha = 'p4ss';\nreturn {};"}}, "code:senha"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			issues := plainSecretIssues(singleNodeGraph(c.node))
			if len(issues) != 1 || issues[0].Path != c.path {
				t.Fatalf("issues = %+v, want path %q", issues, c.path)
			}
		})
	}
}

func TestLintWarnsAboutCredentialsTypedIntoASavedTestScenario(t *testing.T) {
	trigger := Node{ID: "t", Type: NodeTypeTriggerWebhook, TestInputs: []TestInput{
		{ID: "s1", Name: "Sem senha", Input: map[string]interface{}{"nome": "Ana", "token": "{{secret.k}}"}},
		{ID: "s2", Name: "Com senha", Input: map[string]interface{}{"nome": "Ana", "auth": map[string]interface{}{"password": "p4ss"}}},
	}}
	g := &Graph{Nodes: []Node{trigger, {ID: "end", Type: NodeTypeEnd}}, Edges: []Edge{{Source: "t", Target: "end"}}}

	issues := plainSecretIssues(g)
	if len(issues) != 1 {
		t.Fatalf("expected one issue for the scenario holding a password, got %+v", issues)
	}
	issue := issues[0]
	if issue.NodeID != "t" || issue.Field != "testInputs" || issue.Path != "testInputs.Com senha.auth.password" || issue.Severity != SeverityAdvisory {
		t.Fatalf("unexpected shape: %+v", issue)
	}
	if !strings.HasPrefix(issue.Message, "Segredo em texto no fluxo") || !strings.Contains(issue.Message, "Com senha") {
		t.Fatalf("message must open with the rule name and name the scenario: %q", issue.Message)
	}
	if issue.Hint == "" {
		t.Fatalf("hint must say what to do")
	}
}
