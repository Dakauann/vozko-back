package unofficial_whatsapp_campaign

import (
	"context"
	"errors"
	"testing"

	uwc "vozko/domain/unofficial_whatsapp_campaign"
	convuc "vozko/usecases/conversation"
)

// fakeConversationCampaigns is the campaign stored on each conversation.
type fakeConversationCampaigns map[string]string

func (f fakeConversationCampaigns) CampaignIDForEntry(_ context.Context, entryID string) (string, error) {
	if entryID == "conv-broken" {
		return "", errors.New("db down")
	}
	return f[entryID], nil
}

func TestAutomationSourceMapsTheOwningCampaign(t *testing.T) {
	campaigns := newFakeCampaignRepo()
	campaigns.put(&uwc.Campaign{
		ID: "camp-1", AgentID: "agent-1", EnableAgentResponses: true,
		WorkflowID: "wf-1", EnableWorkflow: true,
		EnableAnalysis: true, EnableAutoMemory: true, EnableAutoStaging: true,
	})

	got, ok := NewAutomationSource(fakeConversationCampaigns{"conv-1": "camp-1"}, campaigns).AutomationForConversation("conv-1")
	if !ok {
		t.Fatal("no campaign found for a conversation a campaign opened")
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

func TestAutomationSourceFollowsTheCampaignBeforeAnySendIsRecorded(t *testing.T) {
	// A campaign's conversation exists before its first send is recorded. Read
	// from the entries it looked campaign-less in that window and followed the
	// instance's AI, answering a campaign that has none.
	campaigns := newFakeCampaignRepo()
	campaigns.put(&uwc.Campaign{ID: "camp-no-ai"})

	got, ok := NewAutomationSource(fakeConversationCampaigns{"conv-new": "camp-no-ai"}, campaigns).AutomationForConversation("conv-new")
	if !ok {
		t.Fatal("the campaign's conversation was treated as campaign-less")
	}
	if got.Automation.RunsAgent(nil) || got.Automation.RunsWorkflow(nil) {
		t.Fatalf("a campaign without AI must not run any: %+v", got.Automation)
	}
}

func TestAnalysisResolverUsesCampaignFlagsInsteadOfInstance(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		campaigns := newFakeCampaignRepo()
		campaigns.put(&uwc.Campaign{ID: "camp", Name: "Campaign", AgentID: "agent", AiModel: "model", EnableAnalysis: enabled, EnableAutoMemory: enabled, EnableAutoStaging: enabled})
		stored := fakeConversationCampaigns{"conv": "camp"}
		base := func(context.Context, string) (*convuc.AnalysisSubject, error) {
			return &convuc.AnalysisSubject{EntryID: "conv", ContainerID: "instance", EnableAnalysis: !enabled, EnableAutoMemory: !enabled, EnableAutoStaging: !enabled}, nil
		}
		got, err := NewAutomationSource(stored, campaigns).AnalysisResolver(base)(context.Background(), "conv")
		if err != nil {
			t.Fatal(err)
		}
		if got.ContainerID != "camp" || got.EnableAnalysis != enabled || got.EnableAutoMemory != enabled || got.EnableAutoStaging != enabled || got.AIModel != "model" || got.AgentID != "agent" {
			t.Fatalf("wrong effective config: %+v", got)
		}
		organic, err := NewAutomationSource(stored, campaigns).AnalysisResolver(base)(context.Background(), "organic")
		if err != nil || organic.ContainerID != "instance" || organic.EnableAnalysis != !enabled {
			t.Fatalf("organic config changed: %+v %v", organic, err)
		}
	}
}

func TestAnalysisResolverSurfacesALookupFailure(t *testing.T) {
	base := func(context.Context, string) (*convuc.AnalysisSubject, error) {
		return &convuc.AnalysisSubject{EntryID: "conv-broken"}, nil
	}
	if _, err := NewAutomationSource(fakeConversationCampaigns{}, newFakeCampaignRepo()).AnalysisResolver(base)(context.Background(), "conv-broken"); err == nil {
		t.Fatal("a failed campaign lookup must not be read as a campaign-less conversation")
	}
}

func TestAutomationSourceReportsOrganicConversations(t *testing.T) {
	src := NewAutomationSource(fakeConversationCampaigns{}, newFakeCampaignRepo())
	if _, ok := src.AutomationForConversation("conv-unknown"); ok {
		t.Fatal("claimed a campaign owns a conversation no campaign opened")
	}
	if _, ok := src.AutomationForConversation(""); ok {
		t.Fatal("claimed a campaign owns an empty conversation id")
	}
}

func TestAutomationSourceFallsBackWhenTheCampaignIsGone(t *testing.T) {
	src := NewAutomationSource(fakeConversationCampaigns{"conv-1": "camp-vanished"}, newFakeCampaignRepo())
	if _, ok := src.AutomationForConversation("conv-1"); ok {
		t.Fatal("returned automation for a campaign that no longer exists")
	}
}

func TestAutomationSourceNilIsInert(t *testing.T) {
	var src *AutomationSource
	if _, ok := src.AutomationForConversation("conv-1"); ok {
		t.Fatal("a nil source claimed a campaign")
	}
}

func TestAutomationSourceRunsNothingWhenTheCampaignCannotBeRead(t *testing.T) {
	// "No campaign" would hand the conversation to the instance's AI. A failed
	// read is not "no campaign": run no automation until it can be read.
	got, ok := NewAutomationSource(fakeConversationCampaigns{}, newFakeCampaignRepo()).AutomationForConversation("conv-broken")
	if !ok {
		t.Fatal("a failed lookup fell back to the instance's automation")
	}
	if got.Automation.RunsAgent(nil) || got.Automation.RunsWorkflow(nil) || got.EnableAnalysis || got.EnableAutoMemory || got.EnableAutoStaging {
		t.Fatalf("a failed lookup must run nothing: %+v", got)
	}
}
