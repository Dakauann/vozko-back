package googlesheet

import (
	"context"
	"errors"
	"strings"

	"vozko/brand"
	"vozko/domain/conversation"
	"vozko/domain/googlesheet"
	"vozko/domain/lead"
	"vozko/domain/shared"
)

type ContextValuesDeps struct {
	Leads           lead.Repository
	EntryLeads      lead.EntryLeads
	AgentNames      NameLookup
	WorkflowNames   NameLookup
	FrontendBaseURL string
}

type ContextValues struct {
	leads         lead.Repository
	entryLeads    lead.EntryLeads
	agentNames    NameLookup
	workflowNames NameLookup
	frontend      string
}

var _ googlesheet.SystemValueResolver = (*ContextValues)(nil)

func NewContextValues(d ContextValuesDeps) *ContextValues {
	if d.Leads == nil || d.EntryLeads == nil || d.AgentNames == nil || d.WorkflowNames == nil || strings.TrimSpace(d.FrontendBaseURL) == "" {
		panic("googlesheet: context values require leads, entry leads, agent and workflow names and the frontend url")
	}
	return &ContextValues{
		leads: d.Leads, entryLeads: d.EntryLeads, agentNames: d.AgentNames, workflowNames: d.WorkflowNames,
		frontend: strings.TrimRight(d.FrontendBaseURL, "/"),
	}
}

func (c *ContextValues) Resolve(ctx context.Context, workspaceID string, origin googlesheet.Origin) (googlesheet.SystemValues, error) {
	values := googlesheet.SystemValues{}
	entry := shared.EntryRef{EntryID: origin.EntryID, EntryType: shared.EntryType(origin.EntryType)}
	if entry.EntryID != "" && entry.EntryType != "" {
		values[googlesheet.SourceConversationLink] = c.frontend + conversation.DashboardLink(entry)
	}
	leadID := origin.LeadID
	if leadID == "" && entry.EntryID != "" && entry.EntryType != "" {
		found, err := c.entryLeads.LeadOfEntry(ctx, workspaceID, entry)
		if err != nil && !errors.Is(err, lead.ErrLeadNotFound) {
			return nil, err
		}
		leadID = found
	}
	if leadID != "" {
		person, err := c.leads.FindByID(workspaceID, leadID)
		switch {
		case err == nil:
			values[googlesheet.SourceLeadName] = person.RealName()
			values[googlesheet.SourceLeadEmail] = person.Email
			if number := strings.TrimSpace(person.Number); number != "" {
				values[googlesheet.SourceLeadPhone] = "+" + number
			}
		case !errors.Is(err, lead.ErrLeadNotFound):
			return nil, err
		}
	}
	name, err := c.originName(ctx, workspaceID, origin)
	if err != nil {
		return nil, err
	}
	values[googlesheet.SourceOrigin] = name
	return values, nil
}

func (c *ContextValues) originName(ctx context.Context, workspaceID string, origin googlesheet.Origin) (string, error) {
	switch origin.Kind {
	case googlesheet.OriginAgent:
		return c.agentNames(ctx, workspaceID, origin.AgentID)
	case googlesheet.OriginWorkflow:
		return c.workflowNames(ctx, workspaceID, origin.WorkflowID)
	case googlesheet.OriginCopilot:
		return brand.Active().AIName, nil
	}
	return "Manual", nil
}
