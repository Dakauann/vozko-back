package carriermessaging_usecase

import (
	"context"
	"errors"

	"vozko/domain/conversation"
	"vozko/domain/rcs"
	"vozko/domain/sms"
	"vozko/usecases/shared/automationrefs"
)

var ErrAutomationIncomplete = errors.New("carrier messaging: an enabled automation needs its agent or workflow")

type AutomationRefs interface {
	OwnedBy(ctx context.Context, workspaceID string, refs automationrefs.Refs) error
}

type InboxSettings struct {
	DepartmentID *string
	PipelineID   *string
	Automation   conversation.ChannelAutomation
}

func (s *Service) checkInbox(ctx context.Context, workspaceID string, in InboxSettings) error {
	a := in.Automation
	if (a.EnableAgentResponses && a.AgentID == nil) || (a.EnableWorkflow && a.WorkflowID == nil) {
		return ErrAutomationIncomplete
	}
	return s.AutomationRefs.OwnedBy(ctx, workspaceID, automationrefs.Refs{
		AgentID: a.AgentID, WorkflowID: a.WorkflowID, PipelineID: in.PipelineID, DepartmentID: in.DepartmentID,
	})
}

func (s *Service) SetSenderInbox(ctx context.Context, workspaceID, id string, in InboxSettings) (*rcs.Sender, error) {
	if err := s.checkInbox(ctx, workspaceID, in); err != nil {
		return nil, err
	}
	sender, err := s.Repos.Senders.FindByID(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	sender.DepartmentID, sender.PipelineID, sender.Automation = in.DepartmentID, in.PipelineID, in.Automation
	sender.UpdatedAt = s.Now()
	if err := s.Repos.Senders.Update(ctx, sender); err != nil {
		return nil, err
	}
	return sender, nil
}

func (s *Service) SetRouteInbox(ctx context.Context, workspaceID, id string, in InboxSettings) (*sms.Route, error) {
	if err := s.checkInbox(ctx, workspaceID, in); err != nil {
		return nil, err
	}
	route, err := s.Repos.Routes.FindByID(ctx, workspaceID, id)
	if err != nil {
		return nil, err
	}
	route.DepartmentID, route.PipelineID, route.Automation = in.DepartmentID, in.PipelineID, in.Automation
	return s.saveRoute(ctx, *route)
}
