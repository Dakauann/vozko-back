package workflow

import (
	"testing"
	"time"
)

var alertNow = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

func TestEachRuntimeAlertKindCarriesItsSeverityAndFixAction(t *testing.T) {
	cases := []struct {
		kind     RuntimeAlertKind
		severity RuntimeAlertSeverity
		fix      AlertFixAction
	}{
		{AlertWorkflowCalledInactive, RuntimeAlertHigh, FixPublish},
		{AlertNodeFailing, RuntimeAlertHigh, FixOpenRun},
		{AlertWebhookRejecting, RuntimeAlertMedium, FixViewDeliveries},
		{AlertRunStuck, RuntimeAlertMedium, FixOpenRun},
		{AlertAgentToolFailing, RuntimeAlertMedium, FixOpenAgent},
		{AlertInboundLookupFailed, RuntimeAlertMedium, FixOpenConversation},
	}
	for _, tc := range cases {
		if !tc.kind.Valid() || tc.kind.Severity() != tc.severity || tc.kind.FixAction() != tc.fix {
			t.Errorf("%s: valid=%v severity=%s fix=%s", tc.kind, tc.kind.Valid(), tc.kind.Severity(), tc.kind.FixAction())
		}
	}
	if RuntimeAlertKind("lint").Valid() {
		t.Fatal("an unknown kind must not be valid, or a typo would store alerts nobody can act on")
	}
}

func signalOf(kind RuntimeAlertKind, count int, last time.Time) RuntimeAlertSignal {
	return RuntimeAlertSignal{
		Key:       RuntimeAlertKey{WorkspaceID: "ws", WorkflowID: "wf", NodeID: "n1", Kind: kind},
		Count:     count,
		FirstSeen: last.Add(-time.Minute),
		LastSeen:  last,
		Sample:    "run-1",
	}
}

func TestANewAlertOpensOnlyOnceItsKindThresholdIsReached(t *testing.T) {
	cases := []struct {
		kind  RuntimeAlertKind
		below int
		at    int
	}{
		{AlertWorkflowCalledInactive, 0, 1},
		{AlertNodeFailing, NodeFailingMinFailures - 1, NodeFailingMinFailures},
		{AlertWebhookRejecting, WebhookRejectingMinRejections - 1, WebhookRejectingMinRejections},
		{AlertAgentToolFailing, AgentToolFailingMinErrors - 1, AgentToolFailingMinErrors},
	}
	for _, tc := range cases {
		under := ObserveRuntimeAlerts(nil, nil, []RuntimeAlertSignal{signalOf(tc.kind, tc.below, alertNow)})
		if len(under.Opened) != 0 {
			t.Errorf("%s opened with %d occurrences, below its threshold", tc.kind, tc.below)
		}
		reached := ObserveRuntimeAlerts(nil, nil, []RuntimeAlertSignal{signalOf(tc.kind, tc.at, alertNow)})
		if len(reached.Opened) != 1 {
			t.Fatalf("%s did not open with %d occurrences", tc.kind, tc.at)
		}
		got := reached.Opened[0]
		if got.Status != RuntimeAlertOpen || got.Count != tc.at || got.Sample != "run-1" || !got.LastSeen.Equal(alertNow) || got.Severity != tc.kind.Severity() || got.FixAction != tc.kind.FixAction() {
			t.Errorf("%s opened as %+v", tc.kind, got)
		}
	}
}

func TestAnOpenAlertIsTouchedByAnyNewerFailureEvenBelowTheThreshold(t *testing.T) {
	open := RuntimeAlert{ID: "a1", Key: signalOf(AlertNodeFailing, 3, alertNow).Key, Count: 3, LastSeen: alertNow.Add(-time.Hour), Status: RuntimeAlertOpen}
	changes := ObserveRuntimeAlerts([]RuntimeAlert{open}, nil, []RuntimeAlertSignal{signalOf(AlertNodeFailing, 1, alertNow)})
	if len(changes.Opened) != 0 || len(changes.Touched) != 1 {
		t.Fatalf("changes = %+v", changes)
	}
	if got := changes.Touched[0]; got.ID != "a1" || got.Count != 1 || !got.LastSeen.Equal(alertNow) || got.Sample != "run-1" {
		t.Fatalf("touched = %+v", got)
	}
	same := ObserveRuntimeAlerts([]RuntimeAlert{changes.Touched[0]}, nil, []RuntimeAlertSignal{signalOf(AlertNodeFailing, 1, alertNow)})
	if len(same.Touched) != 0 || len(same.Opened) != 0 {
		t.Fatal("a signal with nothing newer must not rewrite the alert on every tick")
	}
}

func TestFailuresFromBeforeTheLastResolutionNeverReopenTheAlert(t *testing.T) {
	key := signalOf(AlertWorkflowCalledInactive, 4, alertNow).Key
	resolved := map[RuntimeAlertKey]time.Time{key: alertNow}
	old := ObserveRuntimeAlerts(nil, resolved, []RuntimeAlertSignal{signalOf(AlertWorkflowCalledInactive, 4, alertNow.Add(-time.Minute))})
	if len(old.Opened) != 0 {
		t.Fatal("refusals counted before the workflow was published must not reopen the alert on the next tick")
	}
	fresh := ObserveRuntimeAlerts(nil, resolved, []RuntimeAlertSignal{signalOf(AlertWorkflowCalledInactive, 4, alertNow.Add(time.Minute))})
	if len(fresh.Opened) != 1 {
		t.Fatal("a refusal after the resolution must open a new alert")
	}
}

func TestSignalsOfTheSameConditionAddUpBeforeTheThreshold(t *testing.T) {
	webhook := signalOf(AlertWebhookRejecting, 3, alertNow.Add(-time.Minute))
	webhook.Sample = "older"
	agent := signalOf(AlertWebhookRejecting, 2, alertNow)
	agent.Sample = "newer"
	changes := ObserveRuntimeAlerts(nil, nil, []RuntimeAlertSignal{webhook, agent})
	if len(changes.Opened) != 1 {
		t.Fatalf("3 + 2 rejections of one condition reach the threshold of 5, got %+v", changes)
	}
	got := changes.Opened[0]
	if got.Count != 5 || got.Sample != "newer" || !got.FirstSeen.Equal(webhook.FirstSeen) || !got.LastSeen.Equal(alertNow) {
		t.Fatalf("merged = %+v", got)
	}
}

func unreadSource(facts RuntimeAlertFacts, source string) RuntimeAlertFacts {
	switch source {
	case "deliveries":
		facts.DeliveriesRead = false
	case "tool_calls":
		facts.ToolCallsRead = false
	}
	return facts
}

func TestRuntimeAlertsResolveWhenTheirConditionClears(t *testing.T) {
	one := 1
	live := &Workflow{ID: "wf", WorkspaceID: "ws", Status: WorkflowStatusActive, PublishedVersion: &one}
	paused := &Workflow{ID: "wf", WorkspaceID: "ws", Status: WorkflowStatusPaused, PublishedVersion: &one}
	draft := &Workflow{ID: "wf", WorkspaceID: "ws", Status: WorkflowStatusDraft}
	foreign := &Workflow{ID: "wf", WorkspaceID: "other", Status: WorkflowStatusDraft}
	alert := func(kind RuntimeAlertKind, lastSeen time.Time) RuntimeAlert {
		return RuntimeAlert{ID: "a", Key: RuntimeAlertKey{WorkspaceID: "ws", WorkflowID: "wf", NodeID: "n1", Kind: kind}, LastSeen: lastSeen, Status: RuntimeAlertOpen}
	}
	read := func(w *Workflow) RuntimeAlertFacts {
		facts := RuntimeAlertFacts{WorkflowsRead: true, Workflows: map[string]*Workflow{}, DeliveriesRead: true, ToolCallsRead: true}
		if w != nil {
			facts.Workflows[w.ID] = w
		}
		return facts
	}
	withSuccesses := func(n int) RuntimeAlertFacts {
		facts := read(draft)
		facts.NodeStatsRead = true
		facts.NodeSuccesses = map[RuntimeAlertKey]int{alert(AlertNodeFailing, alertNow).Key: n}
		return facts
	}
	cases := []struct {
		name  string
		alert RuntimeAlert
		facts RuntimeAlertFacts
		want  bool
	}{
		{"called inactive, now active and published", alert(AlertWorkflowCalledInactive, alertNow), read(live), true},
		{"called inactive, still paused", alert(AlertWorkflowCalledInactive, alertNow), read(paused), false},
		{"called inactive, never published", alert(AlertWorkflowCalledInactive, alertNow), read(draft), false},
		{"called inactive, workflows unreadable", alert(AlertWorkflowCalledInactive, alertNow), RuntimeAlertFacts{}, false},
		{"workflow deleted", alert(AlertNodeFailing, alertNow), read(nil), true},
		{"workflow of another workspace", alert(AlertNodeFailing, alertNow), read(foreign), true},
		{"node with three successes", alert(AlertNodeFailing, alertNow), withSuccesses(NodeRecoverySuccesses), true},
		{"node with two successes", alert(AlertNodeFailing, alertNow), withSuccesses(NodeRecoverySuccesses - 1), false},
		{"node stats unreadable", alert(AlertNodeFailing, alertNow), read(draft), false},
		{"webhook quiet for an hour", alert(AlertWebhookRejecting, alertNow.Add(-QuietAlertResolveAfter)), read(draft), true},
		{"webhook rejected recently", alert(AlertWebhookRejecting, alertNow.Add(-QuietAlertResolveAfter+time.Minute)), read(draft), false},
		{"agent tool quiet for an hour", alert(AlertAgentToolFailing, alertNow.Add(-QuietAlertResolveAfter)), read(draft), true},
		{"webhook looks quiet only because the deliveries were unreadable", alert(AlertWebhookRejecting, alertNow.Add(-QuietAlertResolveAfter)), unreadSource(read(draft), "deliveries"), false},
		{"agent tool looks quiet only because the tool calls were unreadable", alert(AlertAgentToolFailing, alertNow.Add(-QuietAlertResolveAfter)), unreadSource(read(draft), "tool_calls"), false},
		{"webhook quiet while only the tool calls were unreadable", alert(AlertWebhookRejecting, alertNow.Add(-QuietAlertResolveAfter)), unreadSource(read(draft), "tool_calls"), true},
		{"stuck run a day ago", alert(AlertRunStuck, alertNow.Add(-StuckRunAlertResolveAfter)), read(draft), true},
		{"stuck run an hour ago", alert(AlertRunStuck, alertNow.Add(-time.Hour)), read(draft), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.alert.ResolvedBy(tc.facts, alertNow); got != tc.want {
				t.Fatalf("resolved = %v, want %v", got, tc.want)
			}
		})
	}
	inbound := RuntimeAlert{Key: RuntimeAlertKey{WorkspaceID: "ws", Subject: "instagram", Kind: AlertInboundLookupFailed}, LastSeen: alertNow.Add(-QuietAlertResolveAfter), Status: RuntimeAlertOpen}
	if !inbound.ResolvedBy(RuntimeAlertFacts{}, alertNow) {
		t.Fatal("an inbound lookup alert has no workflow and resolves after an hour without new failures")
	}
}

func TestRejectedDeliveriesSplitIntoCalledInactiveAndWebhookRejecting(t *testing.T) {
	groups := []RejectedDeliveryGroup{
		{WorkspaceID: "ws", WorkflowID: "wf", Reason: WorkflowNotActiveCode, Count: 2, FirstSeen: alertNow, LastSeen: alertNow, Sample: "d-1"},
		{WorkspaceID: "ws", WorkflowID: "wf", Reason: "unauthorized", Count: 7, FirstSeen: alertNow, LastSeen: alertNow, Sample: "d-2"},
	}
	signals := SignalsOfRejectedDeliveries(groups)
	if len(signals) != 2 {
		t.Fatalf("signals = %+v", signals)
	}
	if got := signals[0].Key; got != (RuntimeAlertKey{WorkspaceID: "ws", WorkflowID: "wf", Kind: AlertWorkflowCalledInactive}) {
		t.Fatalf("inactive key = %+v", got)
	}
	if got := signals[1].Key; got != (RuntimeAlertKey{WorkspaceID: "ws", WorkflowID: "wf", Subject: "unauthorized", Kind: AlertWebhookRejecting}) {
		t.Fatalf("rejecting key = %+v", got)
	}
}

func TestFailedToolCallsNameTheirWorkflowFromTheBoundToolName(t *testing.T) {
	groups := []FailedToolCallGroup{
		{WorkspaceID: "ws", AgentID: "ag", ToolName: "run_workflow_wf-1", ErrorCode: WorkflowNotActiveCode, Count: 1, LastSeen: alertNow, Sample: "c-1"},
		{WorkspaceID: "ws", AgentID: "ag", ToolName: "run_workflow_wf-2", Count: 3, LastSeen: alertNow, Sample: "c-2"},
		{WorkspaceID: "ws", AgentID: "ag", ToolName: AgentHTTPRequestTool, Count: 4, LastSeen: alertNow, Sample: "c-3"},
		{WorkspaceID: "ws", AgentID: "ag", ToolName: AgentHTTPRequestTool, ErrorCode: WorkflowNotActiveCode, Count: 1, LastSeen: alertNow},
		{WorkspaceID: "ws", AgentID: "ag", ToolName: "transfer_to_human", Count: 9, LastSeen: alertNow},
	}
	signals := SignalsOfFailedToolCalls(groups)
	want := []RuntimeAlertKey{
		{WorkspaceID: "ws", WorkflowID: "wf-1", Kind: AlertWorkflowCalledInactive},
		{WorkspaceID: "ws", WorkflowID: "wf-2", AgentID: "ag", Subject: "run_workflow_wf-2", Kind: AlertAgentToolFailing},
		{WorkspaceID: "ws", AgentID: "ag", Subject: AgentHTTPRequestTool, Kind: AlertAgentToolFailing},
	}
	if len(signals) != len(want) {
		t.Fatalf("signals = %+v", signals)
	}
	for i, k := range want {
		if signals[i].Key != k {
			t.Errorf("signal %d key = %+v, want %+v", i, signals[i].Key, k)
		}
	}
}

func TestEventSignalsCarryTheirSample(t *testing.T) {
	run := &WorkflowRun{ID: "r-1", WorkflowID: "wf", WorkspaceID: "ws", CurrentNodeID: "n7"}
	stuck := StuckRunSignal(run, alertNow)
	if stuck.Key != (RuntimeAlertKey{WorkspaceID: "ws", WorkflowID: "wf", Kind: AlertRunStuck}) || stuck.Sample != "r-1" || stuck.Count != 1 {
		t.Fatalf("stuck = %+v", stuck)
	}
	inbound := InboundLookupFailedSignal(TriggerEvent{WorkspaceID: "ws", EntryID: "e-1", EntryType: "instagram"}, alertNow)
	if inbound.Key != (RuntimeAlertKey{WorkspaceID: "ws", Subject: "instagram", Kind: AlertInboundLookupFailed}) || inbound.Sample != "e-1" {
		t.Fatalf("inbound = %+v", inbound)
	}
	if InboundLookupFailedSignal(TriggerEvent{EntryID: "e-1"}, alertNow).Valid() {
		t.Fatal("a signal without a workspace cannot be stored")
	}
}

func TestTheEpisodeKeyFollowsTheAlertRow(t *testing.T) {
	if got := (RuntimeAlert{ID: "a-1"}).EpisodeKey(); got != "workflow_alert:a-1" {
		t.Fatalf("episode key = %q", got)
	}
}

func TestTheSampleIsReadAsARunOrAConversationByKind(t *testing.T) {
	node := RuntimeAlert{Key: RuntimeAlertKey{Kind: AlertNodeFailing}, Sample: "run-1"}
	stuck := RuntimeAlert{Key: RuntimeAlertKey{Kind: AlertRunStuck}, Sample: "run-2"}
	webhook := RuntimeAlert{Key: RuntimeAlertKey{Kind: AlertWebhookRejecting}, Sample: "d-1"}
	if node.RunID() != "run-1" || stuck.RunID() != "run-2" || webhook.RunID() != "" {
		t.Fatal("only node and stuck run alerts sample a run")
	}
	inbound := RuntimeAlert{Key: RuntimeAlertKey{Kind: AlertInboundLookupFailed, Subject: "instagram"}, Sample: "e-1"}
	ref, ok := inbound.Conversation()
	if !ok || ref.EntryID != "e-1" || string(ref.EntryType) != "instagram" {
		t.Fatalf("conversation = %+v, %v", ref, ok)
	}
	if _, ok := node.Conversation(); ok {
		t.Fatal("a node alert names no conversation")
	}
}
