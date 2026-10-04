package conversation_usecase

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/agent"
	"vozko/domain/conversation"
	"vozko/domain/lead"
	businessphone "vozko/domain/whatsapp/business_phone"
	wc "vozko/domain/whatsapp_campaign"
	wce "vozko/domain/whatsapp_campaign_entry"
)

const receptiveMetaID = "meta-phone-1"

type routingPhones struct {
	businessphone.Repository
	phone *businessphone.WhatsAppBusinessPhoneNumber
}

func (r routingPhones) FindByMetaPhoneNumberID(string) (*businessphone.WhatsAppBusinessPhoneNumber, error) {
	if r.phone == nil {
		return nil, businessphone.ErrPhoneNumberNotFound
	}
	return r.phone, nil
}

type routingEntries struct {
	wce.Repository
	existing *wce.WhatsAppCampaignEntry
	created  *wce.WhatsAppCampaignEntry
}

func (r *routingEntries) FindByNumberAndBusinessPhone(string, string) (*wce.WhatsAppCampaignEntry, error) {
	if r.existing == nil {
		return nil, wce.ErrEntryNotFound
	}
	return r.existing, nil
}

func (r *routingEntries) Create(e *wce.WhatsAppCampaignEntry) error {
	r.created = e
	return nil
}

type routingCampaigns struct {
	wc.Repository
	byID      map[string]*wc.Campaign
	receptive *wc.Campaign
}

func (r routingCampaigns) FindLatestOrganicByBusinessPhone(string, string) (*wc.Campaign, error) {
	if r.receptive == nil {
		return nil, wc.ErrCampaignNotFound
	}
	return r.receptive, nil
}

func (r routingCampaigns) FindByID(id string) (*wc.Campaign, error) {
	if c, ok := r.byID[id]; ok {
		return c, nil
	}
	return nil, wc.ErrCampaignNotFound
}

type routingLeads struct {
	lead.Repository
}

func (routingLeads) FindByNumber(workspaceID, number string) (*lead.Lead, error) {
	return &lead.Lead{ID: "lead-" + workspaceID, WorkspaceID: workspaceID, Number: number}, nil
}

func (routingLeads) FindOrCreate(workspaceID, number string, _ lead.LeadUpdate) (*lead.Lead, bool, error) {
	return &lead.Lead{ID: "lead-" + workspaceID, WorkspaceID: workspaceID, Number: number}, true, nil
}

type routingReceptive struct {
	container *wc.Campaign
	err       error
	asked     string
}

func (r *routingReceptive) Execute(workspaceID, businessPhoneID, _ string) (*wc.Campaign, bool, error) {
	r.asked = workspaceID + "/" + businessPhoneID
	return r.container, r.container != nil, r.err
}

func receptiveNumber() *businessphone.WhatsAppBusinessPhoneNumber {
	return &businessphone.WhatsAppBusinessPhoneNumber{ID: "phone-1", OwnerWorkspaceID: "owner", DisplayPhoneNumber: "+55 11 4000 1234"}
}

func routingUseCase(phone *businessphone.WhatsAppBusinessPhoneNumber, entries *routingEntries, campaigns map[string]*wc.Campaign, receptive *routingReceptive) *handleWhatsAppMessageUseCase {
	uc := &handleWhatsAppMessageUseCase{
		businessPhoneRepo: routingPhones{phone: phone},
		wcEntryRepo:       entries,
		wcCampaignRepo:    routingCampaigns{byID: campaigns},
		leadRepo:          routingLeads{},
		delegations:       stubDelegations{},
		agentRepo:         agentsByID{byID: map[string]*agent.Agent{"agent-1": {ID: "agent-1", MessagingPrompt: "prompt"}}},
	}
	if receptive != nil {
		uc.SetReceptiveContainers(receptive)
	}
	return uc
}

func resolve(uc *handleWhatsAppMessageUseCase) (*agentContext, *lead.Lead) {
	return uc.resolveAgentContext("5511999990000", &conversation.WhatsAppMetadata{PhoneNumberID: receptiveMetaID})
}

func answeringReceptive(workspaceID string) *wc.Campaign {
	return &wc.Campaign{ID: "receptive-" + workspaceID, WorkspaceID: workspaceID, BusinessPhoneID: "phone-1", Type: wc.CampaignTypeOrganic, AgentID: "agent-1", EnableAgentResponses: true}
}

func TestAReceptiveConversationOfTheOwnerIsAnsweredByTheNumbersAgent(t *testing.T) {
	container := answeringReceptive("owner")
	entries := &routingEntries{existing: &wce.WhatsAppCampaignEntry{ID: "entry-1", CampaignID: container.ID, CreatedAt: time.Now()}}
	actx, _ := resolve(routingUseCase(receptiveNumber(), entries, map[string]*wc.Campaign{container.ID: container}, nil))
	if actx == nil || actx.skipResponse || actx.agent == nil || actx.agent.ID != "agent-1" {
		t.Fatalf("context %+v", actx)
	}
}

func TestAReceptiveConversationOfAGrantedWorkspaceRunsNoAutomation(t *testing.T) {
	container := answeringReceptive("granted")
	container.WorkflowID, container.EnableWorkflow = "flow-1", true
	entries := &routingEntries{existing: &wce.WhatsAppCampaignEntry{ID: "entry-1", CampaignID: container.ID, CreatedAt: time.Now()}}
	actx, _ := resolve(routingUseCase(receptiveNumber(), entries, map[string]*wc.Campaign{container.ID: container}, nil))
	if actx == nil {
		t.Fatal("the message must still be recorded in the granted workspace's conversation")
	}
	if !actx.skipResponse || actx.agent != nil || actx.firesWorkflow() {
		t.Fatalf("a granted workspace's receptive answered: agent %+v fires %v", actx.agent, actx.firesWorkflow())
	}
}

func TestAnOutboundCampaignConversationKeepsTheCampaignAutomation(t *testing.T) {
	campaign := &wc.Campaign{ID: "camp-1", WorkspaceID: "granted", BusinessPhoneID: "phone-1", Type: wc.CampaignTypeStandard, AgentID: "agent-1", EnableAgentResponses: true}
	entries := &routingEntries{existing: &wce.WhatsAppCampaignEntry{ID: "entry-1", CampaignID: campaign.ID, CreatedAt: time.Now()}}
	actx, _ := resolve(routingUseCase(receptiveNumber(), entries, map[string]*wc.Campaign{campaign.ID: campaign}, nil))
	if actx == nil || actx.skipResponse || actx.agent == nil {
		t.Fatalf("context %+v", actx)
	}
}

func TestAConversationSwitchedOffStaysSilent(t *testing.T) {
	container := answeringReceptive("owner")
	off := false
	entries := &routingEntries{existing: &wce.WhatsAppCampaignEntry{ID: "entry-1", CampaignID: container.ID, AutomationEnabled: &off, CreatedAt: time.Now()}}
	actx, _ := resolve(routingUseCase(receptiveNumber(), entries, map[string]*wc.Campaign{container.ID: container}, nil))
	if actx == nil || !actx.skipResponse {
		t.Fatalf("context %+v", actx)
	}
}

func TestTheFirstMessageToANumberOpensAConversationInItsReceptive(t *testing.T) {
	container := answeringReceptive("owner")
	receptive := &routingReceptive{container: container}
	entries := &routingEntries{}
	actx, leadRecord := resolve(routingUseCase(receptiveNumber(), entries, nil, receptive))
	if receptive.asked != "owner/phone-1" {
		t.Fatalf("the container must belong to the number's owner, asked %q", receptive.asked)
	}
	if actx == nil || entries.created == nil || entries.created.CampaignID != container.ID || leadRecord == nil || leadRecord.WorkspaceID != "owner" {
		t.Fatalf("context %+v entry %+v", actx, entries.created)
	}
	if actx.skipResponse || actx.agent == nil {
		t.Fatalf("the number's agent must answer the first message: %+v", actx)
	}
}

func TestTheFirstMessageIsNotAnsweredWhenTheNumberHasNoAutomation(t *testing.T) {
	receptive := &routingReceptive{container: &wc.Campaign{ID: "receptive-owner", WorkspaceID: "owner", Type: wc.CampaignTypeOrganic}}
	entries := &routingEntries{}
	actx, _ := resolve(routingUseCase(receptiveNumber(), entries, nil, receptive))
	if actx == nil || entries.created == nil || !actx.skipResponse {
		t.Fatalf("the message must be recorded without an answer: %+v", actx)
	}
}

func TestTheFirstMessageIsNotRoutedWithoutAnOwnerOrAContainer(t *testing.T) {
	cases := []struct {
		name      string
		phone     *businessphone.WhatsAppBusinessPhoneNumber
		receptive *routingReceptive
	}{
		{"shared number without owner or receptive", &businessphone.WhatsAppBusinessPhoneNumber{ID: "phone-1"}, &routingReceptive{container: answeringReceptive("owner")}},
		{"container unavailable", receptiveNumber(), &routingReceptive{err: errors.New("database down")}},
		{"no container service", receptiveNumber(), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entries := &routingEntries{}
			actx, _ := resolve(routingUseCase(tc.phone, entries, nil, tc.receptive))
			if actx != nil || entries.created != nil {
				t.Fatalf("context %+v entry %+v", actx, entries.created)
			}
		})
	}
}

func TestTheFirstMessageToASharedNumberWithoutOwnerIsNotRouted(t *testing.T) {
	shared := &businessphone.WhatsAppBusinessPhoneNumber{ID: "phone-1"}
	receptive := &routingReceptive{container: answeringReceptive("other")}
	entries := &routingEntries{}
	uc := routingUseCase(shared, entries, nil, receptive)
	uc.wcCampaignRepo = routingCampaigns{receptive: answeringReceptive("granted")}

	actx, _ := resolve(uc)
	if actx != nil || entries.created != nil || receptive.asked != "" {
		t.Fatalf("no workspace owns the number, so nobody may receive a new contact: context %+v entry %+v", actx, entries.created)
	}
}

func TestAConversationAlreadyOpenOnASharedNumberStillReachesItsWorkspace(t *testing.T) {
	shared := &businessphone.WhatsAppBusinessPhoneNumber{ID: "phone-1"}
	container := answeringReceptive("granted")
	entries := &routingEntries{existing: &wce.WhatsAppCampaignEntry{ID: "entry-1", CampaignID: container.ID, CreatedAt: time.Now()}}
	actx, leadRecord := resolve(routingUseCase(shared, entries, map[string]*wc.Campaign{container.ID: container}, nil))
	if actx == nil || leadRecord == nil || leadRecord.WorkspaceID != "granted" {
		t.Fatalf("context %+v", actx)
	}
	if !actx.skipResponse || actx.agent != nil || actx.firesWorkflow() {
		t.Fatal("nothing answers automatically on a number without owner")
	}
}
