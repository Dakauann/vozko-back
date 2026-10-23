package workflow

import (
	"strings"
	"testing"
)

var removedTriggerTypes = []string{"trigger_manual", "trigger_no_reply", "trigger_campaign_sent"}

func storedGraphWith(removed string) Graph {
	return Graph{
		Nodes: []Node{
			{ID: "inicio", Type: NodeType(removed)},
			{ID: "ola", Type: NodeTypeActionSendText, Config: map[string]interface{}{"text": "Oi"}},
			{ID: "end", Type: NodeTypeEnd},
		},
		Edges: []Edge{{Source: "inicio", Target: "ola"}, {Source: "ola", Target: "end"}},
	}
}

func TestARemovedTriggerIsNoLongerATriggerType(t *testing.T) {
	for _, removed := range removedTriggerTypes {
		if TriggerType(removed).Valid() || NodeType(removed).Valid() || NodeType(removed).IsTrigger() {
			t.Fatalf("%s is still accepted", removed)
		}
		if _, ok := EntryKindOfTrigger(TriggerType(removed)); ok {
			t.Fatalf("%s still produces an entry point", removed)
		}
	}
	for _, kept := range []NodeType{NodeTypeTriggerStageAdded} {
		if !kept.Valid() || !kept.IsTrigger() {
			t.Fatalf("%s must stay a trigger", kept)
		}
	}
}

func TestAStoredGraphWithARemovedTriggerBlocksPublishingAndNamesTheNode(t *testing.T) {
	for _, removed := range removedTriggerTypes {
		t.Run(removed, func(t *testing.T) {
			w := &Workflow{WorkspaceID: "ws1", Name: "Antigo", TriggerType: TriggerType(removed), Graph: storedGraphWith(removed)}
			w.Normalize()
			if len(w.Graph.Nodes) != 3 || w.Graph.Nodes[0].Type != NodeType(removed) {
				t.Fatalf("nodes = %+v, the removed trigger must stay visible in the editor", w.Graph.Nodes)
			}
			var invalid *LintIssue
			for _, issue := range LintGraph(&w.Graph, WorkflowTypeMessages, nil, nil, nil).Issues {
				if issue.Code == LintInvalidNodeType {
					issue := issue
					invalid = &issue
				}
			}
			if invalid == nil || invalid.Severity != SeverityBlocking || invalid.NodeID != "inicio" || !strings.Contains(invalid.Message, removed) {
				t.Fatalf("issue = %+v, want a blocking invalid type naming node inicio", invalid)
			}
		})
	}
}

func TestAWorkflowStoredWithARemovedTriggerCanStillBeSavedPausedOrArchived(t *testing.T) {
	for _, removed := range removedTriggerTypes {
		w := &Workflow{WorkspaceID: "ws1", Name: "Antigo", Status: WorkflowStatusPaused, TriggerType: TriggerType(removed), Graph: storedGraphWith(removed)}
		w.Normalize()
		if err := w.Validate(); err != nil {
			t.Fatalf("%s: validate = %v, want the draft writable so the trigger can be replaced", removed, err)
		}
		if w.TriggerType != "" {
			t.Fatalf("%s: trigger type = %q, want the unknown legacy column cleared", removed, w.TriggerType)
		}
	}
}

func TestAReplacedTriggerTakesOverTheLegacyColumn(t *testing.T) {
	g := storedGraphWith("trigger_manual")
	g.Nodes = append(g.Nodes, Node{ID: "hook", Type: NodeTypeTriggerWebhook})
	w := &Workflow{WorkspaceID: "ws1", Name: "Antigo", TriggerType: "trigger_manual", Graph: g}
	w.Normalize()
	if w.TriggerType != TriggerWebhook {
		t.Fatalf("trigger type = %q", w.TriggerType)
	}
}
