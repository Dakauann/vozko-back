package whatsapp_campaign

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrReceptiveManagedByNumber = errors.New("receptive service is configured on the business number, not as a campaign")
	ErrReceptiveNotOwner        = errors.New("only the workspace that owns the business number configures its receptive service")
)

type ReceptiveSettings struct {
	AgentID              string
	WorkflowID           string
	PipelineID           string
	EnableAgentResponses bool
	EnableWorkflow       bool
	EnableAnalysis       bool
	EnableAutoStaging    bool
	EnableAutoMemory     bool
}

func (s *ReceptiveSettings) Normalize() {
	s.AgentID = strings.TrimSpace(s.AgentID)
	s.WorkflowID = strings.TrimSpace(s.WorkflowID)
	s.PipelineID = strings.TrimSpace(s.PipelineID)
}

func (c *Campaign) Receptive() ReceptiveSettings {
	return ReceptiveSettings{
		AgentID:              c.AgentID,
		WorkflowID:           c.WorkflowID,
		PipelineID:           c.PipelineID,
		EnableAgentResponses: c.EnableAgentResponses,
		EnableWorkflow:       c.EnableWorkflow,
		EnableAnalysis:       c.EnableAnalysis,
		EnableAutoStaging:    c.EnableAutoStaging,
		EnableAutoMemory:     c.EnableAutoMemory,
	}
}

func (c *Campaign) RunsAutomationOn(numberOwnerWorkspaceID string) bool {
	if !c.IsOrganic() {
		return true
	}
	owner := strings.TrimSpace(numberOwnerWorkspaceID)
	return owner != "" && owner == strings.TrimSpace(c.WorkspaceID)
}

func NewReceptiveContainer(workspaceID, businessPhoneID, displayNumber string, now time.Time) *Campaign {
	return &Campaign{
		ID:              uuid.New().String(),
		WorkspaceID:     workspaceID,
		BusinessPhoneID: businessPhoneID,
		Name:            "Receptivo " + strings.TrimSpace(displayNumber),
		Type:            CampaignTypeOrganic,
		Status:          CampaignStatusRunning,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

type Answerer string

const (
	AnsweredByAgent    Answerer = "agent"
	AnsweredByWorkflow Answerer = "workflow"
	AnsweredByNobody   Answerer = "none"
)

func (s ReceptiveSettings) AnsweredBy(who Answerer, id string) ReceptiveSettings {
	next := s
	next.EnableAgentResponses, next.EnableWorkflow = false, false
	switch who {
	case AnsweredByAgent:
		next.AgentID, next.EnableAgentResponses = id, true
	case AnsweredByWorkflow:
		next.WorkflowID, next.EnableWorkflow = id, true
	}
	return next
}

func (s ReceptiveSettings) Answerer() Answerer {
	switch {
	case s.EnableWorkflow && s.WorkflowID != "":
		return AnsweredByWorkflow
	case s.EnableAgentResponses && s.AgentID != "":
		return AnsweredByAgent
	default:
		return AnsweredByNobody
	}
}
