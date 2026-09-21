package unofficial_whatsapp

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"vozko/domain/campaign"
	"vozko/domain/conversation"
	uw "vozko/domain/unofficial_whatsapp"
	"vozko/domain/workflow"
)

type recordingWorkflows struct {
	mu     sync.Mutex
	events []workflow.TriggerEvent
}

func (w *recordingWorkflows) Evaluate(ev workflow.TriggerEvent) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.events = append(w.events, ev)
}

func (w *recordingWorkflows) all() []workflow.TriggerEvent {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]workflow.TriggerEvent(nil), w.events...)
}

type recordingAI struct {
	mu       sync.Mutex
	requests []conversation.AIReplyRequest
}

func (a *recordingAI) Reply(_ context.Context, req conversation.AIReplyRequest) (*conversation.Message, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.requests = append(a.requests, req)
	return nil, nil
}

func (a *recordingAI) all() []conversation.AIReplyRequest {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]conversation.AIReplyRequest(nil), a.requests...)
}

type stubAutomationSource struct {
	found *CampaignAutomation
}

func (s *stubAutomationSource) AutomationForConversation(string) (*CampaignAutomation, bool) {
	if s.found == nil {
		return nil, false
	}
	return s.found, true
}

type autoHarness struct {
	uc        *HandleWebhookUseCase
	workflows *recordingWorkflows
	ai        *recordingAI
	convs     *fakeConversationRepo
}

func newAutoHarness(t *testing.T, src CampaignAutomationSource) *autoHarness {
	t.Helper()

	instAgent := "inst-agent"
	instWorkflow := "inst-workflow"
	instance := &uw.Instance{
		ID: "inst-1", WorkspaceID: "ws-1", ServerID: "srv-1",
		Status: uw.StatusConnected, PhoneNumber: "5599999999999",
		AgentID: &instAgent, EnableAgentResponses: true,
		WorkflowID: &instWorkflow, EnableWorkflow: true,
	}
	h := &autoHarness{
		workflows: &recordingWorkflows{},
		ai:        &recordingAI{},
		convs:     newFakeConversationRepo(),
	}
	h.uc = NewHandleWebhookUseCase(HandleWebhookDeps{
		Instances:     newFakeInstanceRepo(instance),
		Servers:       newFakeServerRepo(&uw.Server{ID: "srv-1", BaseURL: "https://host.test"}),
		Contacts:      newFakeContactRepo(),
		Conversations: h.convs,
		Groups:        newFakeGroupRepo(),
		Messaging:     &fakeMessaging{},
		GroupAPI:      &fakeGroupAPI{},
		Assets:        &fakeAssets{},
		FileStorage:   newFakeStorage(),
		History:       &recordingHistory{},
		Workflows:     h.workflows,
		AIReply:       h.ai,
	})
	if src != nil {
		h.uc.SetCampaignAutomationSource(src)
	}
	return h
}

func (h *autoHarness) deliverInbound(t *testing.T) {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"event": "messages", "instance": "prov-1",
		"data": map[string]any{
			"messageid":        "msg-1",
			"chatid":           "558494409624@s.whatsapp.net",
			"sender":           "558494409624@s.whatsapp.net",
			"sender_pn":        "558494409624@s.whatsapp.net",
			"senderName":       "Dakauann",
			"isGroup":          false,
			"messageType":      "text",
			"text":             "oi",
			"messageTimestamp": time.Now().UnixMilli(),
		},
	})
	if err != nil {
		t.Fatalf("encoding the provider message: %v", err)
	}
	if err := h.uc.Execute(context.Background(), &QueuedEvent{InstanceID: "inst-1", Body: body}); err != nil {
		t.Fatalf("delivering the inbound message: %v", err)
	}
}

func campaignAuto(a campaign.Automation) *stubAutomationSource {
	return &stubAutomationSource{found: &CampaignAutomation{CampaignID: "camp-1", Automation: a}}
}

func TestCampaignWithNoAutomationSilencesTheInstance(t *testing.T) {
	h := newAutoHarness(t, campaignAuto(campaign.Automation{}))
	h.deliverInbound(t)

	if events := h.workflows.all(); len(events) != 0 {
		t.Fatalf("fired %d workflow triggers, want 0 — the instance's workflow leaked into a manual campaign", len(events))
	}
	if reqs := h.ai.all(); len(reqs) != 0 {
		t.Fatalf("made %d AI replies, want 0 — the instance's agent leaked into a manual campaign", len(reqs))
	}
}

func TestCampaignAgentAnswersInsteadOfTheInstanceAgent(t *testing.T) {
	h := newAutoHarness(t, campaignAuto(campaign.Automation{
		AgentID: "camp-agent", EnableAgentResponses: true,
	}))
	h.deliverInbound(t)

	reqs := h.ai.all()
	if len(reqs) != 1 {
		t.Fatalf("made %d AI replies, want 1", len(reqs))
	}
	if reqs[0].AgentID != "camp-agent" {
		t.Fatalf("replied with agent %q, want the CAMPAIGN's agent", reqs[0].AgentID)
	}
	if events := h.workflows.all(); len(events) != 0 {
		t.Fatalf("fired %d workflow triggers alongside the agent, want 0", len(events))
	}
}

func TestCampaignWorkflowIsScopedAndSuppressesTheAgent(t *testing.T) {
	h := newAutoHarness(t, campaignAuto(campaign.Automation{
		AgentID: "camp-agent", EnableAgentResponses: true,
		WorkflowID: "camp-workflow", EnableWorkflow: true,
	}))
	h.deliverInbound(t)

	events := h.workflows.all()
	if len(events) != 1 {
		t.Fatalf("fired %d workflow triggers, want 1", len(events))
	}
	if got := events[0].Data["campaign_workflow_id"]; got != "camp-workflow" {
		t.Fatalf("campaign_workflow_id = %v, want the campaign's workflow — every workspace workflow would answer", got)
	}
	if got := events[0].Data["campaign_id"]; got != "camp-1" {
		t.Fatalf("campaign_id = %v, want camp-1", got)
	}
	if reqs := h.ai.all(); len(reqs) != 0 {
		t.Fatalf("made %d AI replies while a workflow was running, want 0 — the customer gets two answers", len(reqs))
	}
}

func TestOrganicConversationStillUsesTheInstance(t *testing.T) {
	h := newAutoHarness(t, &stubAutomationSource{found: nil})
	h.deliverInbound(t)

	events := h.workflows.all()
	if len(events) != 1 {
		t.Fatalf("fired %d workflow triggers, want 1", len(events))
	}
	if got := events[0].Data["account_workflow_id"]; got != "inst-workflow" {
		t.Fatalf("account_workflow_id = %v, want the instance's workflow", got)
	}
	if _, scoped := events[0].Data["campaign_workflow_id"]; scoped {
		t.Fatal("an organic conversation carried campaign_workflow_id")
	}
}

func TestNoAutomationSourceFallsBackToTheInstance(t *testing.T) {
	h := newAutoHarness(t, nil)
	h.deliverInbound(t)

	if events := h.workflows.all(); len(events) != 1 {
		t.Fatalf("fired %d workflow triggers, want 1", len(events))
	}
	if reqs := h.ai.all(); len(reqs) != 1 {
		t.Fatalf("made %d AI replies, want 1", len(reqs))
	}
}
