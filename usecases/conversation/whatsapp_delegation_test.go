package conversation_usecase

import (
	"errors"
	"testing"

	"vozko/domain/agent"
	"vozko/domain/conversation"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
)

type agentsByID struct {
	agent.Repository
	byID map[string]*agent.Agent
}

func (r agentsByID) FindByID(id string) (*agent.Agent, error) {
	if a, ok := r.byID[id]; ok {
		return a, nil
	}
	return nil, errors.New("not found")
}

func campaignContext() *agentContext {
	return &agentContext{
		wcCampaign: &wc.Campaign{ID: "camp-1", AgentID: "camp-agent", WorkflowID: "camp-wf", EnableWorkflow: true},
		wcEntry:    &wce.WhatsAppCampaignEntry{ID: "entry-1"},
	}
}

func whatsappUseCase(delegations conversation.DelegationRepository) *handleWhatsAppMessageUseCase {
	return &handleWhatsAppMessageUseCase{
		delegations: delegations,
		agentRepo: agentsByID{byID: map[string]*agent.Agent{
			"camp-agent": {ID: "camp-agent"},
			"picked":     {ID: "picked", MessagingPrompt: "prompt"},
		}},
	}
}

func TestCampaignWorkflowRunsWithoutADelegation(t *testing.T) {
	actx := campaignContext()
	responds := whatsappUseCase(stubDelegations{}).prepareAutomation(actx, true)
	if responds || !actx.skipResponse || !actx.firesWorkflow() || actx.automation.WorkflowRef() != "camp-wf" {
		t.Fatalf("responds %v, automation %+v", responds, actx.automation)
	}
}

func TestADelegatedAgentAnswersAnOfficialCampaignInsteadOfItsWorkflow(t *testing.T) {
	actx := campaignContext()
	responds := whatsappUseCase(delegatedTo(conversation.AutomationAgent, "picked")).prepareAutomation(actx, false)
	if !responds || actx.skipResponse || actx.firesWorkflow() || actx.agent == nil || actx.agent.ID != "picked" {
		t.Fatalf("responds %v, agent %+v, fires workflow %v", responds, actx.agent, actx.firesWorkflow())
	}
}

func TestADelegatedWorkflowReplacesTheCampaignWorkflow(t *testing.T) {
	actx := campaignContext()
	whatsappUseCase(delegatedTo(conversation.AutomationWorkflow, "picked-wf")).prepareAutomation(actx, true)
	if !actx.firesWorkflow() || actx.automation.WorkflowRef() != "picked-wf" || actx.agent != nil {
		t.Fatalf("automation %+v, agent %+v", actx.automation, actx.agent)
	}
}

func TestAnUnreadableDelegationSilencesTheCampaign(t *testing.T) {
	actx := campaignContext()
	responds := whatsappUseCase(stubDelegations{err: errors.New("db down")}).prepareAutomation(actx, true)
	if responds || !actx.skipResponse || actx.firesWorkflow() {
		t.Fatalf("an unknown delegation let the campaign automation run: responds %v fires %v", responds, actx.firesWorkflow())
	}
}
