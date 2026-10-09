package shared

import (
	"strings"
	"testing"
)

func leadContext(phone, name, conversationID, campaign string, metadata map[string]interface{}) ConversationContext {
	return ConversationContext{
		Channel:         ChannelWhatsApp,
		UserPhoneNumber: phone,
		UserName:        name,
		ConversationID:  conversationID,
		CampaignName:    campaign,
		AgentName:       "Bia",
		Metadata:        metadata,
		AvailableTools:  []string{"manage_entry_stage", "search_knowledge_base"},
	}
}

func TestRulesAreTheSameForEveryLeadOfTheSameAgent(t *testing.T) {
	ana := leadContext("5511999990001", "Rosalinda", "conv-ana", "Promo Ana", map[string]interface{}{"cidade": "Recife"})
	bruno := leadContext("5511999990002", "Bruno", "conv-bruno", "Promo Bruno", map[string]interface{}{"cidade": "Natal"})

	if ana.RulesPrompt() != bruno.RulesPrompt() {
		t.Fatalf("rules differ between leads:\n%s\n---\n%s", ana.RulesPrompt(), bruno.RulesPrompt())
	}
	for _, leadFact := range []string{"5511999990001", "Rosalinda", "conv-ana", "Promo Ana", "Recife"} {
		if strings.Contains(ana.RulesPrompt(), leadFact) {
			t.Errorf("rules carry the lead fact %q:\n%s", leadFact, ana.RulesPrompt())
		}
	}
}

func TestLeadBlockCarriesEveryLeadFact(t *testing.T) {
	ana := leadContext("5511999990001", "Rosalinda", "conv-ana", "Promo Ana", map[string]interface{}{"cidade": "Recife"})

	lead := ana.LeadPrompt()
	for _, want := range []string{
		"Lead WhatsApp Number: 5511999990001",
		"Lead Name: Rosalinda",
		"Conversation ID: conv-ana",
		"Campaign: Promo Ana",
		"cidade: Recife",
	} {
		if !strings.Contains(lead, want) {
			t.Errorf("lead block is missing %q:\n%s", want, lead)
		}
	}
	if strings.Contains(lead, "REGRAS DE IDENTIDADE") {
		t.Errorf("lead block carries the rules:\n%s", lead)
	}
}

func TestLeadMetadataIsListedInKeyOrderEveryTime(t *testing.T) {
	metadata := map[string]interface{}{}
	for _, key := range []string{"zona", "alfa", "meio", "bairro", "cpf", "estado", "plano", "origem"} {
		metadata[key] = key + "-valor"
	}
	ana := leadContext("5511999990001", "Rosalinda", "conv-ana", "", metadata)

	first := ana.LeadPrompt()
	for round := 0; round < 20; round++ {
		if got := ana.LeadPrompt(); got != first {
			t.Fatalf("round %d: metadata order changed:\n%s\n---\n%s", round, first, got)
		}
	}

	previous := -1
	for _, key := range []string{"alfa", "bairro", "cpf", "estado", "meio", "origem", "plano", "zona"} {
		at := strings.Index(first, key+": ")
		if at < 0 || at < previous {
			t.Fatalf("metadata key %q is out of order:\n%s", key, first)
		}
		previous = at
	}
}

func TestRuleSectionsFollowTheAgentsTools(t *testing.T) {
	withoutTools := ConversationContext{Channel: ChannelWhatsApp}
	if strings.Contains(withoutTools.RulesPrompt(), "USO DE FERRAMENTAS") {
		t.Error("a toolless agent was told to call tools")
	}

	staging := ConversationContext{Channel: ChannelWhatsApp, AvailableTools: []string{"manage_entry_stage"}}
	if !strings.Contains(staging.RulesPrompt(), "CLASSIFICAÇÃO DO LEAD") {
		t.Error("the stage classification rule is missing for an agent with the stage tool")
	}

	messaging := ConversationContext{Channel: ChannelMessaging, AvailableTools: []string{"manage_entry_stage"}}
	if strings.Contains(messaging.RulesPrompt(), "CLASSIFICAÇÃO DO LEAD") {
		t.Error("the messaging channel must not get the stage classification rule")
	}
}
