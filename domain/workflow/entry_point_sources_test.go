package workflow

import (
	"reflect"
	"sort"
	"testing"
	"time"
)

func TestTheEntryPointsOfAPublishedWorkflowComeFromItsPublishedTriggers(t *testing.T) {
	published := entryWorkflow(WorkflowStatusActive, true)
	published.Graph = Graph{Nodes: []Node{triggerOf(NodeTypeTriggerManual, nil)}}
	draft := entryWorkflow(WorkflowStatusDraft, false)
	draft.ID = "wf-2"
	draft.Graph = Graph{Nodes: []Node{triggerOf(NodeTypeTriggerWebhook, nil), {ID: "end", Type: NodeTypeEnd}}}
	triggers := map[string][]Node{"wf-1": {triggerOf(NodeTypeTriggerAgentCall, nil)}}

	if got := RunningTriggers(published, triggers); len(got) != 1 || got[0].Type != NodeTypeTriggerAgentCall {
		t.Fatalf("published triggers = %+v, want the published agent trigger, not the draft", got)
	}
	if got := RunningTriggers(draft, triggers); len(got) != 1 || got[0].Type != NodeTypeTriggerWebhook {
		t.Fatalf("never published triggers = %+v, want the draft triggers", got)
	}
	unreadable := entryWorkflow(WorkflowStatusActive, true)
	unreadable.ID = "wf-3"
	unreadable.Graph = published.Graph
	if got := RunningTriggers(unreadable, triggers); len(got) != 0 {
		t.Fatalf("a published version that could not be read must not fall back to the draft: %+v", got)
	}
}

func TestTheSourceQueryAsksForEveryChosenFormAndCampaignOnce(t *testing.T) {
	a := entryWorkflow(WorkflowStatusActive, true)
	b := entryWorkflow(WorkflowStatusDraft, false)
	b.ID = "wf-2"
	b.Graph = Graph{Nodes: []Node{
		triggerOf(NodeTypeTriggerFormSubmitted, map[string]interface{}{FormTriggerFormIDsKey: []interface{}{"f-2", "f-1"}}),
		triggerOf(NodeTypeTriggerCampaignSent, map[string]interface{}{CampaignTriggerCampaignIDKey: " c-1 "}),
	}}
	triggers := map[string][]Node{"wf-1": {triggerOf(NodeTypeTriggerFormSubmitted, map[string]interface{}{FormTriggerFormIDsKey: []interface{}{"f-1"}})}}

	q := EntryPointQueryOf("ws-1", []*Workflow{a, b, nil}, triggers)
	sort.Strings(q.FormIDs)
	if q.WorkspaceID != "ws-1" || !reflect.DeepEqual(q.WorkflowIDs, []string{"wf-1", "wf-2"}) || !reflect.DeepEqual(q.FormIDs, []string{"f-1", "f-2"}) || !reflect.DeepEqual(q.CampaignIDs, []string{"c-1"}) {
		t.Fatalf("query = %+v", q)
	}
}

func TestTheFactsOfOneWorkflowSplitTheWorkspaceChannels(t *testing.T) {
	at := time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC)
	sources := &EntryPointSources{
		Webhooks: map[string]WebhookEntry{"wf-1": {Active: true}},
		Agents:   map[string][]EntryAgent{"wf-1": {{ID: "a-1", Active: true}}, "wf-2": {{ID: "a-2"}}},
		LinkedChannels: []WorkspaceChannel{
			{EntryChannel: EntryChannel{Kind: ChannelFacebookPage, ID: "p-1", WorkflowEnabled: true}, WorkflowID: "wf-1"},
			{EntryChannel: EntryChannel{Kind: ChannelWhatsAppCampaign, ID: "c-1"}, WorkflowID: "wf-2"},
		},
		EnabledChannels: []WorkspaceChannel{
			{EntryChannel: EntryChannel{Kind: ChannelWebchatWidget, ID: "w-1", WorkflowEnabled: true}},
			{EntryChannel: EntryChannel{Kind: ChannelUnofficialWhatsAppCampaign, ID: "u-1", WorkflowEnabled: true}, WorkflowID: "wf-9"},
			{EntryChannel: EntryChannel{Kind: ChannelTelegramAccount, ID: "t-1", WorkflowEnabled: true}, WorkflowID: "wf-9"},
		},
		LastStarts:     map[string]map[EntryPointKind]time.Time{"wf-1": {EntryManual: at}},
		LastDeliveries: map[string]EntryDelivery{"wf-1": {At: at, Outcome: WebhookOutcomeAccepted}},
	}
	facts := sources.FactsOf("wf-1")
	if facts.Webhook == nil || !facts.Webhook.Active || len(facts.Agents) != 1 || facts.LastDelivery == nil || !facts.LastStarts[EntryManual].Equal(at) {
		t.Fatalf("facts = %+v", facts)
	}
	if len(facts.Channels) != 1 || facts.Channels[0].ID != "p-1" {
		t.Fatalf("bound channels = %+v, want only the ones bound to wf-1", facts.Channels)
	}
	if len(facts.OpenChannels) != 1 || facts.OpenChannels[0].ID != "w-1" {
		t.Fatalf("open channels = %+v, want only enabled channels bound to no workflow", facts.OpenChannels)
	}
	if len(facts.EnabledCampaigns) != 1 || facts.EnabledCampaigns[0].ID != "u-1" {
		t.Fatalf("enabled campaigns = %+v, want every campaign with workflows on", facts.EnabledCampaigns)
	}
	if c, ok := facts.ConfiguredCampaigns["c-1"]; !ok || c.WorkflowEnabled {
		t.Fatalf("configured campaigns = %+v, want every linked campaign by id", facts.ConfiguredCampaigns)
	}
	if other := sources.FactsOf("wf-3"); other.Webhook != nil || len(other.Channels) != 0 || other.LastDelivery != nil {
		t.Fatalf("a workflow with nothing linked = %+v", other)
	}
}
