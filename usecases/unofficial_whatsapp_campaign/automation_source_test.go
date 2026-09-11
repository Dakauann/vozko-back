package unofficial_whatsapp_campaign

import (
	"context"
	"testing"
	"time"

	uwc "vozko/domain/unofficial_whatsapp_campaign"
	convuc "vozko/usecases/conversation"
)

func TestAutomationSourceMapsTheOwningCampaign(t *testing.T) {
	entries := newFakeEntryRepo()
	sent := time.Now()
	entries.put(&uwc.Entry{
		ID: "e1", CampaignID: "camp-1", ConversationID: "conv-1", SentAt: &sent,
	})
	campaigns := newFakeCampaignRepo()
	campaigns.put(&uwc.Campaign{
		ID: "camp-1", AgentID: "agent-1", EnableAgentResponses: true,
		WorkflowID: "wf-1", EnableWorkflow: true,
		EnableAnalysis: true, EnableAutoMemory: true, EnableAutoStaging: true,
	})

	got, ok := NewAutomationSource(entries, campaigns).AutomationForConversation("conv-1")
	if !ok {
		t.Fatal("no campaign found for a conversation a campaign targeted")
	}
	if got.CampaignID != "camp-1" {
		t.Fatalf("CampaignID = %q, want camp-1", got.CampaignID)
	}
	if got.Automation.AgentID != "agent-1" || !got.Automation.EnableAgentResponses {
		t.Fatalf("agent config not carried across: %+v", got.Automation)
	}
	if got.Automation.WorkflowID != "wf-1" || !got.Automation.EnableWorkflow {
		t.Fatalf("workflow config not carried across: %+v", got.Automation)
	}
	if !got.EnableAnalysis || !got.EnableAutoMemory || !got.EnableAutoStaging {
		t.Fatal("lost campaign enrichment flags")
	}
}

func TestAnalysisResolverUsesCampaignFlagsInsteadOfInstance(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		entries, campaigns := newFakeEntryRepo(), newFakeCampaignRepo()
		sent := time.Now()
		entries.put(&uwc.Entry{ID: "e", CampaignID: "camp", ConversationID: "conv", SentAt: &sent})
		campaigns.put(&uwc.Campaign{ID: "camp", Name: "Campaign", AgentID: "agent", AiModel: "model", EnableAnalysis: enabled, EnableAutoMemory: enabled, EnableAutoStaging: enabled})
		base := func(context.Context, string) (*convuc.AnalysisSubject, error) {
			return &convuc.AnalysisSubject{EntryID: "conv", ContainerID: "instance", EnableAnalysis: !enabled, EnableAutoMemory: !enabled, EnableAutoStaging: !enabled}, nil
		}
		got, err := NewAutomationSource(entries, campaigns).AnalysisResolver(base)(context.Background(), "conv")
		if err != nil {
			t.Fatal(err)
		}
		if got.ContainerID != "camp" || got.EnableAnalysis != enabled || got.EnableAutoMemory != enabled || got.EnableAutoStaging != enabled || got.AIModel != "model" || got.AgentID != "agent" {
			t.Fatalf("wrong effective config: %+v", got)
		}
		organic, err := NewAutomationSource(entries, campaigns).AnalysisResolver(base)(context.Background(), "organic")
		if err != nil || organic.ContainerID != "instance" || organic.EnableAnalysis != !enabled {
			t.Fatalf("organic config changed: %+v %v", organic, err)
		}
	}
}

// Two campaigns targeting the same person on the same instance: the reply
// belongs to the one they actually received last.
func TestAutomationSourceLatestSendWins(t *testing.T) {
	older := time.Now().Add(-2 * time.Hour)
	newer := time.Now()

	entries := newFakeEntryRepo()
	entries.put(&uwc.Entry{ID: "e-old", CampaignID: "camp-old", ConversationID: "conv-1", SentAt: &older})
	entries.put(&uwc.Entry{ID: "e-new", CampaignID: "camp-new", ConversationID: "conv-1", SentAt: &newer})
	// A third campaign queued the same person but never sent. It cannot own a
	// reply to a message it never delivered.
	entries.put(&uwc.Entry{ID: "e-unsent", CampaignID: "camp-unsent", ConversationID: "conv-1"})

	campaigns := newFakeCampaignRepo()
	for _, id := range []string{"camp-old", "camp-new", "camp-unsent"} {
		campaigns.put(&uwc.Campaign{ID: id})
	}

	got, ok := NewAutomationSource(entries, campaigns).AutomationForConversation("conv-1")
	if !ok {
		t.Fatal("no campaign found")
	}
	if got.CampaignID != "camp-new" {
		t.Fatalf("CampaignID = %q, want camp-new — the reply follows the last message received", got.CampaignID)
	}
}

// An organic conversation is not a campaign conversation, and must be reported
// as such rather than guessed at: the instance configures it.
func TestAutomationSourceReportsOrganicConversations(t *testing.T) {
	src := NewAutomationSource(newFakeEntryRepo(), newFakeCampaignRepo())
	if _, ok := src.AutomationForConversation("conv-unknown"); ok {
		t.Fatal("claimed a campaign owns a conversation no campaign targeted")
	}
	if _, ok := src.AutomationForConversation(""); ok {
		t.Fatal("claimed a campaign owns an empty conversation id")
	}
}

// A campaign row that has gone missing under a live entry must not be guessed
// at either. Reporting "not a campaign" restores the instance's behaviour,
// which is the conservative reading of an unknown.
func TestAutomationSourceFallsBackWhenTheCampaignIsGone(t *testing.T) {
	entries := newFakeEntryRepo()
	sent := time.Now()
	entries.put(&uwc.Entry{ID: "e1", CampaignID: "camp-vanished", ConversationID: "conv-1", SentAt: &sent})

	src := NewAutomationSource(entries, newFakeCampaignRepo())
	if _, ok := src.AutomationForConversation("conv-1"); ok {
		t.Fatal("returned automation for a campaign that no longer exists")
	}
}

// Campaigns not wired at all.
func TestAutomationSourceNilIsInert(t *testing.T) {
	var src *AutomationSource
	if _, ok := src.AutomationForConversation("conv-1"); ok {
		t.Fatal("a nil source claimed a campaign")
	}
}
