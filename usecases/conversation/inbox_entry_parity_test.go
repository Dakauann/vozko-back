package conversation_usecase

import (
	"testing"

	"github.com/stretchr/testify/require"

	"vozko/domain/audience"
	"vozko/domain/conversation"
	"vozko/domain/livedecision"
)

type parityHistory struct {
	inboxServiceTestHistoryProvider
	entries map[string]conversation.InboxEntry
}

func (p *parityHistory) SearchInboxEntries(conversation.SearchInboxInput) ([]conversation.InboxEntry, int64, error) {
	return []conversation.InboxEntry{p.entries["queued"], p.entries["awaiting"]}, 2, nil
}

func (p *parityHistory) GetInboxEntry(entryID, _ string) (*conversation.InboxEntry, error) {
	entry := p.entries[entryID]
	return &entry, nil
}

type parityStages struct {
	conversation.StageProvider
}

func (parityStages) GetBatchEntryStages(entryIDs []string, _, _ string) (map[string]*conversation.InboxEntryStage, error) {
	out := map[string]*conversation.InboxEntryStage{}
	for _, id := range entryIDs {
		out[id] = &conversation.InboxEntryStage{StageID: "agendado-" + id, Name: "agendado"}
	}
	return out, nil
}

func (parityStages) GetAvailableStages(_ string, placements []conversation.StagePlacement) ([][]conversation.InboxEntryStage, error) {
	out := make([][]conversation.InboxEntryStage, len(placements))
	for i, p := range placements {
		out[i] = []conversation.InboxEntryStage{{StageID: p.CurrentStageID, Name: "agendado"}}
	}
	return out, nil
}

type parityLabels struct {
	conversation.LabelProvider
}

func (parityLabels) GetBatchEntryLabels(entryIDs []string, _, _ string) (map[string][]conversation.InboxEntryLabel, error) {
	out := map[string][]conversation.InboxEntryLabel{}
	for _, id := range entryIDs {
		out[id] = []conversation.InboxEntryLabel{{LabelID: "vip-" + id, Name: "vip"}}
	}
	return out, nil
}

type parityAnalysis struct{}

func (parityAnalysis) GetBatchLatestAnalysis(entryIDs []string, _ string) (map[string]*audience.Analysis, error) {
	out := map[string]*audience.Analysis{}
	for _, id := range entryIDs {
		out[id] = &audience.Analysis{ID: "analysis-" + id}
	}
	return out, nil
}

func (parityAnalysis) GetBatchAnalysisPending(entryIDs []string, _ string) (map[string]bool, error) {
	return map[string]bool{"queued": true}, nil
}

type paritySchedule struct{}

func (paritySchedule) AwaitingAnalysis([]string, string) (map[string]bool, error) {
	return map[string]bool{"queued": true, "awaiting": true}, nil
}

type parityLive struct{}

func (parityLive) LiveReadViews(_ string, entryIDs []string, _ string) (map[string]*livedecision.LiveReadView, error) {
	out := map[string]*livedecision.LiveReadView{}
	for _, id := range entryIDs {
		out[id] = &livedecision.LiveReadView{Qualification: "hot_lead"}
	}
	return out, nil
}

func parityService() conversation.InboxService {
	history := &parityHistory{entries: map[string]conversation.InboxEntry{
		"queued":   {EntryID: "queued", EntryType: "whatsapp", CampaignID: "camp-1"},
		"awaiting": {EntryID: "awaiting", EntryType: "whatsapp", CampaignID: "camp-1"},
	}}
	authorizer := &inboxServiceTestAuthorizer{canAccessCampaign: true, departmentScopeOkay: true}
	service := NewInboxService(history, parityStages{}, parityLabels{}, &inboxServiceTestResolver{workspaceID: "ws-1"}, nil, authorizer, parityAnalysis{}, nil)
	service.(interface {
		SetLiveReadProvider(conversation.LiveReadProvider)
	}).SetLiveReadProvider(parityLive{})
	service.(interface {
		SetAnalysisScheduleReader(conversation.AnalysisScheduleReader)
	}).SetAnalysisScheduleReader(paritySchedule{})
	return service
}

func TestARefreshedConversationMatchesTheSameConversationOnThePage(t *testing.T) {
	service := parityService()

	page, _, err := service.SearchInbox("user-1", conversation.SearchInboxInput{WorkspaceID: "ws-1", Page: 1, PageSize: 20, SortOrder: "desc"})
	require.NoError(t, err)

	for _, listed := range page {
		refreshed, err := service.BuildInboxEntry(listed.EntryID, listed.EntryType)
		require.NoError(t, err)
		require.Equal(t, listed, *refreshed, "a silent refresh must not drop or change anything the page showed")
	}
}

func TestTheAnalysisBadgeFollowsTheWorkInProgress(t *testing.T) {
	service := parityService()

	queued, err := service.BuildInboxEntry("queued", "whatsapp")
	require.NoError(t, err)
	awaiting, err := service.BuildInboxEntry("awaiting", "whatsapp")
	require.NoError(t, err)

	require.Equal(t, conversation.AnalysisPhaseQueued, queued.AnalysisPhase, "a running analysis outranks a waiting one")
	require.Equal(t, conversation.AnalysisPhaseAwaiting, awaiting.AnalysisPhase)
	require.Equal(t, "hot_lead", awaiting.LiveRead.Qualification)
	require.Equal(t, "agendado-awaiting", awaiting.AvailableStages[0].StageID)
	require.Equal(t, "vip", awaiting.Labels[0].Name)
}
