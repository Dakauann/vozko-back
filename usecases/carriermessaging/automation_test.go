package carriermessaging_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/conversation"
	"vozko/usecases/shared/automationrefs"
)

type fakeRefs struct{ allowed map[string]bool }

func (f fakeRefs) OwnedBy(_ context.Context, _ string, refs automationrefs.Refs) error {
	for _, id := range []*string{refs.AgentID, refs.WorkflowID} {
		if id != nil && !f.allowed[*id] {
			return automationrefs.ErrNotInWorkspace
		}
	}
	return nil
}

func TestSetSenderInbox(t *testing.T) {
	h := newHarness(t)
	h.svc.AutomationRefs = fakeRefs{allowed: map[string]bool{"agent-1": true}}
	agent, foreign := "agent-1", "agent-9"

	s, err := h.svc.SetSenderInbox(context.Background(), "ws-1", "sender-1", InboxSettings{Automation: conversation.ChannelAutomation{AgentID: &agent, EnableAgentResponses: true}})
	if err != nil || s.Automation.AgentID == nil || !s.Automation.EnableAgentResponses {
		t.Fatalf("SetSenderAutomation = %+v, %v", s, err)
	}
	if _, err := h.svc.SetSenderInbox(context.Background(), "ws-1", "sender-1", InboxSettings{Automation: conversation.ChannelAutomation{AgentID: &foreign}}); !errors.Is(err, automationrefs.ErrNotInWorkspace) {
		t.Fatalf("foreign agent = %v", err)
	}
	if _, err := h.svc.SetSenderInbox(context.Background(), "ws-1", "sender-1", InboxSettings{Automation: conversation.ChannelAutomation{EnableAgentResponses: true}}); !errors.Is(err, ErrAutomationIncomplete) {
		t.Fatalf("agent replies without an agent = %v", err)
	}
	if _, err := h.svc.SetSenderInbox(context.Background(), "ws-1", "sender-1", InboxSettings{Automation: conversation.ChannelAutomation{EnableWorkflow: true}}); !errors.Is(err, ErrAutomationIncomplete) {
		t.Fatalf("workflow without a workflow = %v", err)
	}
}

func TestSetRouteInbox(t *testing.T) {
	h := newHarness(t)
	h.svc.AutomationRefs = fakeRefs{allowed: map[string]bool{"wf-1": true}}
	wf := "wf-1"
	r, err := h.svc.SetRouteInbox(context.Background(), "ws-1", "route-1", InboxSettings{Automation: conversation.ChannelAutomation{WorkflowID: &wf, EnableWorkflow: true}})
	if err != nil || r.Automation.WorkflowID == nil || !r.Automation.EnableWorkflow {
		t.Fatalf("SetRouteAutomation = %+v, %v", r, err)
	}
}

func TestInboxSettingsCarryDepartmentAndFunnel(t *testing.T) {
	h := newHarness(t)
	dept, funnel, foreign := "dept-1", "funnel-1", "dept-9"
	h.svc.AutomationRefs = fakeInboxRefs{allowed: map[string]bool{"dept-1": true, "funnel-1": true}}
	s, err := h.svc.SetSenderInbox(context.Background(), "ws-1", "sender-1", InboxSettings{DepartmentID: &dept, PipelineID: &funnel})
	if err != nil || *s.DepartmentID != "dept-1" || *s.PipelineID != "funnel-1" {
		t.Fatalf("SetSenderInbox = %+v, %v", s, err)
	}
	if _, err := h.svc.SetRouteInbox(context.Background(), "ws-1", "route-1", InboxSettings{DepartmentID: &foreign}); !errors.Is(err, automationrefs.ErrNotInWorkspace) {
		t.Fatalf("foreign department = %v", err)
	}
}

type fakeInboxRefs struct{ allowed map[string]bool }

func (f fakeInboxRefs) OwnedBy(_ context.Context, _ string, refs automationrefs.Refs) error {
	for _, id := range []*string{refs.AgentID, refs.WorkflowID, refs.PipelineID, refs.DepartmentID} {
		if id != nil && !f.allowed[*id] {
			return automationrefs.ErrNotInWorkspace
		}
	}
	return nil
}
