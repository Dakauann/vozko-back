package workflow

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func entryWorkflow(status WorkflowStatus, published bool) *Workflow {
	w := &Workflow{ID: "wf-1", WorkspaceID: "ws-1", Status: status, Type: WorkflowTypeMessages}
	if published {
		one := 1
		w.PublishedVersion = &one
	}
	return w
}

func triggerOf(t NodeType, config map[string]interface{}) Node {
	return Node{ID: "t-" + string(t), Type: t, Config: config}
}

func activeWebhook() *WebhookEntry {
	return &WebhookEntry{Active: true, AuthMode: WebhookAuthHeaderToken, Method: "POST"}
}

func onlyEntry(t *testing.T, entries []EntryPoint, kind EntryPointKind) EntryPoint {
	t.Helper()
	var found []EntryPoint
	for _, e := range entries {
		if e.Kind == kind {
			found = append(found, e)
		}
	}
	if len(found) != 1 {
		t.Fatalf("entries of kind %s = %+v, want exactly one", kind, entries)
	}
	return found[0]
}

func TestEveryEntryPointKindAndReason(t *testing.T) {
	active, paused, archived, draft := entryWorkflow(WorkflowStatusActive, true), entryWorkflow(WorkflowStatusPaused, true), entryWorkflow(WorkflowStatusArchived, true), entryWorkflow(WorkflowStatusDraft, false)
	enabledBound := EntryChannel{Kind: ChannelUnofficialWhatsAppInstance, ID: "i-1", Name: "Comercial", WorkflowEnabled: true}
	disabledBound := EntryChannel{Kind: ChannelInstagramAccount, ID: "ig-1", Name: "@loja"}
	open := EntryChannel{Kind: ChannelWebchatWidget, ID: "wc-1", Name: "Site", WorkflowEnabled: true}
	formTrigger := func(ids interface{}) Node {
		return triggerOf(NodeTypeTriggerFormSubmitted, map[string]interface{}{FormTriggerFormIDsKey: ids})
	}

	cases := []struct {
		name     string
		w        *Workflow
		triggers []Node
		facts    EntryPointFacts
		kind     EntryPointKind
		reason   EntryBlockReason
	}{
		{"a draft never published refuses every call", draft, []Node{triggerOf(NodeTypeTriggerStageAdded, nil)}, EntryPointFacts{}, EntryStageAdded, EntryBlockedNotPublished},
		{"a paused workflow refuses", paused, []Node{triggerOf(NodeTypeTriggerStageAdded, nil)}, EntryPointFacts{}, EntryStageAdded, EntryBlockedPaused},
		{"an archived workflow refuses like a paused one", archived, []Node{triggerOf(NodeTypeTriggerStageAdded, nil)}, EntryPointFacts{}, EntryStageAdded, EntryBlockedPaused},
		{"stage changes need only the workflow", active, []Node{triggerOf(NodeTypeTriggerStageAdded, nil)}, EntryPointFacts{}, EntryStageAdded, ""},
		{"calls need only the workflow", active, []Node{triggerOf(NodeTypeTriggerCallReceived, nil)}, EntryPointFacts{}, EntryCallReceived, ""},
		{"not published wins over a missing webhook", draft, []Node{triggerOf(NodeTypeTriggerWebhook, nil)}, EntryPointFacts{}, EntryWebhook, EntryBlockedNotPublished},
		{"a webhook trigger without a webhook", active, []Node{triggerOf(NodeTypeTriggerWebhook, nil)}, EntryPointFacts{}, EntryWebhook, EntryBlockedNoWebhook},
		{"a switched off webhook", active, []Node{triggerOf(NodeTypeTriggerWebhook, nil)}, EntryPointFacts{Webhook: &WebhookEntry{AuthMode: WebhookAuthNone, Method: "POST"}}, EntryWebhook, EntryBlockedWebhookInactive},
		{"an active webhook", active, []Node{triggerOf(NodeTypeTriggerWebhook, nil)}, EntryPointFacts{Webhook: activeWebhook()}, EntryWebhook, ""},
		{"an active webhook with no trigger to start", active, []Node{triggerOf(NodeTypeTriggerStageAdded, nil)}, EntryPointFacts{Webhook: activeWebhook()}, EntryWebhook, EntryBlockedNoTrigger},
		{"a form trigger for every form", active, []Node{formTrigger([]interface{}{})}, EntryPointFacts{}, EntryFormSubmitted, ""},
		{"a form that no longer exists", active, []Node{formTrigger([]interface{}{"f-1"})}, EntryPointFacts{}, EntryFormSubmitted, EntryBlockedNoForm},
		{"a form list that cannot be read", active, []Node{formTrigger(5)}, EntryPointFacts{}, EntryFormSubmitted, EntryBlockedNoForm},
		{"a form never published", active, []Node{formTrigger([]interface{}{"f-1"})}, EntryPointFacts{Forms: map[string]EntryForm{"f-1": {ID: "f-1", Name: "Matrícula"}}}, EntryFormSubmitted, EntryBlockedFormInactive},
		{"one live form is enough", active, []Node{formTrigger([]interface{}{"f-1", "f-2"})}, EntryPointFacts{Forms: map[string]EntryForm{"f-1": {ID: "f-1"}, "f-2": {ID: "f-2", Active: true}}}, EntryFormSubmitted, ""},
		{"no agent runs it", active, []Node{triggerOf(NodeTypeTriggerAgentCall, nil)}, EntryPointFacts{}, EntryAgentCall, EntryBlockedNoAgentBound},
		{"only a switched off agent runs it", active, []Node{triggerOf(NodeTypeTriggerAgentCall, nil)}, EntryPointFacts{Agents: []EntryAgent{{ID: "a-1", Name: "Bia"}}}, EntryAgentCall, EntryBlockedNoAgentBound},
		{"an active agent runs it", active, []Node{triggerOf(NodeTypeTriggerAgentCall, nil)}, EntryPointFacts{Agents: []EntryAgent{{ID: "a-1", Name: "Bia", Active: true}}}, EntryAgentCall, ""},
		{"an agent bound to a workflow with no agent trigger", active, []Node{triggerOf(NodeTypeTriggerStageAdded, nil)}, EntryPointFacts{Agents: []EntryAgent{{ID: "a-1", Name: "Bia", Active: true}}}, EntryAgentCall, EntryBlockedNoTrigger},
		{"no channel delivers messages", active, []Node{triggerOf(NodeTypeTriggerMessageReceived, nil)}, EntryPointFacts{}, EntryMessageReceived, EntryBlockedNoChannel},
		{"the bound channel has workflows switched off", active, []Node{triggerOf(NodeTypeTriggerMessageReceived, nil)}, EntryPointFacts{Channels: []EntryChannel{disabledBound}}, EntryMessageReceived, EntryBlockedChannelPaused},
		{"a bound channel runs it", active, []Node{triggerOf(NodeTypeTriggerMessageReceived, nil)}, EntryPointFacts{Channels: []EntryChannel{disabledBound, enabledBound}}, EntryMessageReceived, ""},
		{"a channel that runs every workflow runs it", active, []Node{triggerOf(NodeTypeTriggerFirstMessage, nil)}, EntryPointFacts{Channels: []EntryChannel{disabledBound}, OpenChannels: []EntryChannel{open}}, EntryFirstMessage, ""},
		{"a lead exit starts an active workflow at its message trigger", active, []Node{triggerOf(NodeTypeTriggerStageAdded, nil), triggerOf(NodeTypeTriggerMessageReceived, nil)}, EntryPointFacts{LeadExits: []LeadExitEntry{{Source: LeadExitFromTemplate, Name: "boas_vindas", Button: "Quero falar"}}}, EntryLeadExit, ""},
		{"a first message trigger takes a lead exit too", active, []Node{triggerOf(NodeTypeTriggerFirstMessage, nil)}, EntryPointFacts{LeadExits: []LeadExitEntry{{Source: LeadExitFromTemplate, Name: "boas_vindas", Button: "Quero falar"}}}, EntryLeadExit, ""},
		{"a lead exit into a workflow without a message trigger", active, []Node{triggerOf(NodeTypeTriggerStageAdded, nil), triggerOf(NodeTypeTriggerWebhook, nil)}, EntryPointFacts{LeadExits: []LeadExitEntry{{Source: LeadExitFromTemplate, Name: "boas_vindas", Button: "Quero falar"}}}, EntryLeadExit, EntryBlockedNeedsMessageTrigger},
		{"a lead exit into a paused workflow", paused, []Node{triggerOf(NodeTypeTriggerMessageReceived, nil)}, EntryPointFacts{LeadExits: []LeadExitEntry{{Source: LeadExitFromCampaign, ID: "c-1", Name: "Black Friday", Button: "Sair"}}}, EntryLeadExit, EntryBlockedPaused},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := onlyEntry(t, EntryPointsOf(tc.w, tc.triggers, tc.facts), tc.kind)
			wantReadiness := EntryReady
			if tc.reason != "" {
				wantReadiness = EntryBlocked
			}
			if got.Readiness != wantReadiness || got.Reason != tc.reason {
				t.Fatalf("got %s/%q, want %s/%q", got.Readiness, got.Reason, wantReadiness, tc.reason)
			}
		})
	}
}

func TestTheCalledInactiveAlertAndTheEntryPointsShareOneRule(t *testing.T) {
	for _, tc := range []struct {
		w      *Workflow
		reason EntryBlockReason
	}{
		{entryWorkflow(WorkflowStatusDraft, false), EntryBlockedNotPublished},
		{entryWorkflow(WorkflowStatusActive, false), EntryBlockedNotPublished},
		{entryWorkflow(WorkflowStatusPaused, true), EntryBlockedPaused},
		{entryWorkflow(WorkflowStatusActive, true), ""},
		{nil, EntryBlockedNotPublished},
	} {
		if got := WorkflowBlockReason(tc.w); got != tc.reason {
			t.Fatalf("reason = %q, want %q", got, tc.reason)
		}
		if tc.w.Callable() != (tc.reason == "") {
			t.Fatalf("Callable disagrees with the readiness rule for %+v", tc.w)
		}
	}
}

func TestEntryPointsCarryTheirDetailsAndTheLastCall(t *testing.T) {
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	runAt := at.Add(-time.Hour)
	facts := EntryPointFacts{
		Webhook:      activeWebhook(),
		LastDelivery: &EntryDelivery{At: at, Outcome: WebhookOutcomeRejected, Reason: WorkflowNotActiveCode},
		LastStarts:   map[EntryPointKind]time.Time{EntryWebhook: runAt, EntryAgentCall: runAt, EntryFormSubmitted: runAt},
		Agents:       []EntryAgent{{ID: "a-1", Name: "Bia", Active: true}},
		Forms:        map[string]EntryForm{"f-1": {ID: "f-1", Name: "Matrícula", Active: true}},
	}
	triggers := []Node{
		triggerOf(NodeTypeTriggerWebhook, nil),
		triggerOf(NodeTypeTriggerAgentCall, nil),
		triggerOf(NodeTypeTriggerFormSubmitted, map[string]interface{}{FormTriggerFormIDsKey: []interface{}{"f-1", "f-9"}}),
	}
	entries := EntryPointsOf(entryWorkflow(WorkflowStatusActive, true), triggers, facts)

	webhook := onlyEntry(t, entries, EntryWebhook)
	if webhook.NodeID != "t-trigger_webhook" || webhook.Details.Webhook == nil || webhook.Details.Webhook.AuthMode != WebhookAuthHeaderToken {
		t.Fatalf("webhook = %+v", webhook)
	}
	if webhook.LastReceivedAt == nil || !webhook.LastReceivedAt.Equal(at) || webhook.LastOutcome != WebhookOutcomeRejected || webhook.LastReason != WorkflowNotActiveCode {
		t.Fatalf("a webhook's last call is its last delivery, rejections included: %+v", webhook)
	}
	agent := onlyEntry(t, entries, EntryAgentCall)
	if len(agent.Details.Agents) != 1 || agent.Details.Agents[0].Name != "Bia" || agent.LastReceivedAt == nil || !agent.LastReceivedAt.Equal(runAt) {
		t.Fatalf("agent = %+v", agent)
	}
	form := onlyEntry(t, entries, EntryFormSubmitted)
	if len(form.Details.Forms) != 2 || form.Details.Forms[1].ID != "f-9" || !form.Details.Forms[1].Missing {
		t.Fatalf("a form that is gone stays listed as missing: %+v", form.Details.Forms)
	}
}

func TestOnlyALimitedSampleOfChannelsThatRunEveryWorkflowIsListed(t *testing.T) {
	var open []EntryChannel
	for i := 0; i < MaxListedChannels+5; i++ {
		open = append(open, EntryChannel{Kind: ChannelWebchatWidget, ID: string(rune('a' + i)), WorkflowEnabled: true})
	}
	bound := EntryChannel{Kind: ChannelFacebookPage, ID: "p-1", WorkflowEnabled: true}
	entry := onlyEntry(t, EntryPointsOf(entryWorkflow(WorkflowStatusActive, true), []Node{triggerOf(NodeTypeTriggerMessageReceived, nil)}, EntryPointFacts{Channels: []EntryChannel{bound}, OpenChannels: open}), EntryMessageReceived)
	if len(entry.Details.Channels) != MaxListedChannels || !entry.Details.Channels[0].Bound || entry.Details.Channels[1].Bound {
		t.Fatalf("channels = %+v, want the bound one first and at most %d", entry.Details.Channels, MaxListedChannels)
	}
}

func TestRunOriginsMatchHowRunsRecordWhoStartedThem(t *testing.T) {
	if !strings.HasPrefix(StartedByExit("m-1"), RunOriginPrefix(RunOriginLeadExit)) || !strings.HasPrefix(StartedByManual("u-1"), RunOriginPrefix(RunOriginManual)) {
		t.Fatal("the run origin prefixes must be the ones runs are stamped with")
	}
	for _, origin := range RunOrigins() {
		if _, ok := EntryKindOfRunOrigin(origin); !ok {
			t.Fatalf("origin %q has no entry kind", origin)
		}
	}
	if kind, ok := EntryKindOfRunOrigin(string(TriggerWebhook)); !ok || kind != EntryWebhook {
		t.Fatalf("webhook origin = %s", kind)
	}
	if _, ok := EntryKindOfRunOrigin("agent"); ok {
		t.Fatal("an agent run is counted by its agent trigger, not by its starter")
	}
	if _, ok := EntryKindOfRunOrigin(RunOriginManual); ok {
		t.Fatal("a person starting a run is a run origin, not an entry point the workflow listens to")
	}
	for _, origin := range RunOrigins() {
		if origin == RunOriginManual {
			t.Fatal("operator starts must not be read as the last call of any entry point")
		}
	}
}

func TestALeadExitEntryListsTheButtonsThatStartIt(t *testing.T) {
	exits := []LeadExitEntry{{Source: LeadExitFromTemplate, Name: "boas_vindas", Language: "pt_BR", Button: "Quero falar"}, {Source: LeadExitFromCampaign, ID: "c-1", Name: "Black Friday", Button: "Sair"}}
	entry := onlyEntry(t, EntryPointsOf(entryWorkflow(WorkflowStatusActive, true), []Node{triggerOf(NodeTypeTriggerAgentCall, nil)}, EntryPointFacts{LeadExits: exits}), EntryLeadExit)
	if entry.Reason != EntryBlockedNeedsMessageTrigger || len(entry.Details.LeadExits) != 2 || entry.Details.LeadExits[1].Button != "Sair" {
		t.Fatalf("entry = %+v", entry)
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"reason":"needs_message_trigger"`) {
		t.Fatalf("json = %s", raw)
	}
}

func TestEntryPointsSerialiseForTheEditor(t *testing.T) {
	entries := EntryPointsOf(entryWorkflow(WorkflowStatusPaused, true), []Node{triggerOf(NodeTypeTriggerWebhook, nil)}, EntryPointFacts{Webhook: activeWebhook()})
	raw, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"kind":"webhook","nodeId":"t-trigger_webhook","readiness":"blocked","reason":"paused","details":{"webhook":{"active":true,"authMode":"header_token","method":"POST"}}}]`
	if string(raw) != want {
		t.Fatalf("json = %s\nwant   %s", raw, want)
	}
}
