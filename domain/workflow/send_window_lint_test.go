package workflow

import (
	"reflect"
	"strings"
	"testing"
)

func windowGraph(nodes []Node, edges ...Edge) *Graph {
	return &Graph{Nodes: append(nodes, Node{ID: "end", Type: NodeTypeEnd}), Edges: edges}
}

func linked(source, target string) Edge { return Edge{Source: source, Target: target} }

func linkedBy(source, target, label string) Edge {
	return Edge{Source: source, Target: target, Label: label}
}

func TestAFreeFormSendIsARiskOnlyWhenAClosedWindowTriggerReachesItWithoutAnOpener(t *testing.T) {
	webhook := Node{ID: "hook", Type: NodeTypeTriggerWebhook}
	message := Node{ID: "msg", Type: NodeTypeTriggerMessageReceived}
	text := sendText("text", "Oi")
	other := sendText("other", "Tchau")
	cases := []struct {
		name string
		g    *Graph
		want []WindowRisk
	}{
		{
			"a webhook sending text straight away fails outside the window",
			windowGraph([]Node{webhook, text}, linked("hook", "text"), linked("text", "end")),
			[]WindowRisk{{SendNodeID: "text", TriggerNodeID: "hook"}},
		},
		{
			"a message trigger opens the window",
			windowGraph([]Node{message, text}, linked("msg", "text"), linked("text", "end")),
			nil,
		},
		{
			"an agent call happens inside a conversation",
			windowGraph([]Node{{ID: "agent", Type: NodeTypeTriggerAgentCall}, text}, linked("agent", "text"), linked("text", "end")),
			nil,
		},
		{
			"a template sent first makes the next text a reply to it",
			windowGraph([]Node{webhook, {ID: "tpl", Type: NodeTypeActionSendTemplate}, text}, linked("hook", "tpl"), linked("tpl", "text"), linked("text", "end")),
			nil,
		},
		{
			"only the replied branch of a reply wait opens the window",
			windowGraph([]Node{webhook, {ID: "tpl", Type: NodeTypeActionSendEmail}, {ID: "wait", Type: NodeTypeWaitForReply}, text, other},
				linked("hook", "tpl"), linked("tpl", "wait"), linkedBy("wait", "text", "replied"), linkedBy("wait", "other", "timeout"), linked("text", "end"), linked("other", "end")),
			[]WindowRisk{{SendNodeID: "other", TriggerNodeID: "hook"}},
		},
		{
			"one path without an opener is enough to warn",
			windowGraph([]Node{{ID: "bday", Type: NodeTypeTriggerBirthday}, {ID: "if", Type: NodeTypeConditionBranch}, {ID: "tpl", Type: NodeTypeActionSendTemplate}, text},
				linked("bday", "if"), linkedBy("if", "tpl", "true"), linkedBy("if", "text", "false"), linked("tpl", "text"), linked("text", "end")),
			[]WindowRisk{{SendNodeID: "text", TriggerNodeID: "bday"}},
		},
		{
			"a node shared with a message trigger still warns for the webhook path",
			windowGraph([]Node{message, webhook, text}, linked("msg", "text"), linked("hook", "text"), linked("text", "end")),
			[]WindowRisk{{SendNodeID: "text", TriggerNodeID: "hook"}},
		},
		{
			"email and other channels have no service window",
			windowGraph([]Node{webhook, {ID: "mail", Type: NodeTypeActionSendEmail}}, linked("hook", "mail"), linked("mail", "end")),
			nil,
		},
		{
			"a chosen option of a prompt is a customer reply",
			windowGraph([]Node{{ID: "stage", Type: NodeTypeTriggerStageAdded}, {ID: "ask", Type: NodeTypeActionSendInteractive}, text},
				linked("stage", "ask"), linkedBy("ask", "text", "1"), linkedBy("ask", "end", "no_reply"), linked("text", "end")),
			[]WindowRisk{{SendNodeID: "ask", TriggerNodeID: "stage"}},
		},
		{
			"media, forms and the AI agent are free-form sends too",
			windowGraph([]Node{webhook, {ID: "media", Type: NodeTypeActionSendMedia}, {ID: "form", Type: NodeTypeActionSendForm}, {ID: "ai", Type: NodeTypeActionAIAgent}},
				linked("hook", "media"), linked("media", "ai"), linkedBy("ai", "form", "default"), linkedBy("form", "end", "send_failed")),
			[]WindowRisk{{SendNodeID: "media", TriggerNodeID: "hook"}, {SendNodeID: "form", TriggerNodeID: "hook"}, {SendNodeID: "ai", TriggerNodeID: "hook"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SendOutsideWindowRisks(tc.g); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("risks = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestTheLintWarnsOnTheSendNodeAndNamesTheTrigger(t *testing.T) {
	g := windowGraph([]Node{{ID: "hook", Type: NodeTypeTriggerWebhook, Config: map[string]interface{}{"display_name": "Carrinho abandonado"}}, sendText("text", "Oi")},
		linked("hook", "text"), linked("text", "end"))
	var found []LintIssue
	for _, issue := range LintGraph(g, WorkflowTypeMessages, nil, nil, nil).Issues {
		if issue.Code == LintSendOutsideWindowRisk {
			found = append(found, issue)
		}
	}
	if len(found) != 1 {
		t.Fatalf("issues = %+v, want one window warning", found)
	}
	issue := found[0]
	if issue.Severity != SeverityAdvisory || issue.NodeID != "text" || !strings.Contains(issue.Message, "Carrinho abandonado") || !strings.Contains(issue.Hint, "template") {
		t.Fatalf("issue = %+v", issue)
	}
}
