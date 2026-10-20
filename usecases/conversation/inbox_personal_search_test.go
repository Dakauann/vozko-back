package conversation_usecase

import (
	"testing"

	"github.com/stretchr/testify/require"

	"vozko/domain/conversation"
	"vozko/domain/personalinbox"
)

func TestSearchInboxSearchesAsTheRequesterWhateverTheInputSays(t *testing.T) {
	historyProvider := &inboxServiceTestHistoryProvider{}
	authorizer := &inboxServiceTestAuthorizer{canAccessCampaign: true, departmentScopeOkay: true}
	service := NewInboxService(historyProvider, nil, nil, &inboxServiceTestResolver{workspaceID: "ws-1"}, nil, authorizer, nil, nil)

	_, _, err := service.SearchInbox("user-1", conversation.SearchInboxInput{
		UserID:      "someone-else",
		WorkspaceID: "ws-1",
		Personal:    personalinbox.ViewPinned,
		Page:        1,
		PageSize:    20,
	})

	require.NoError(t, err)
	require.Equal(t, "user-1", historyProvider.lastSearch.UserID, "personal marks are always the requester's own")
}

type capturedSearch struct {
	conversation.MessageRepository
	input conversation.SearchEntriesInput
}

func (c *capturedSearch) SearchEntriesWithMessages(input conversation.SearchEntriesInput) ([]conversation.EntryWithLastMessage, int64, error) {
	c.input = input
	return nil, 0, nil
}

func TestSearchInboxEntriesPassesThePersonalViewSortAndLabelToTheRepository(t *testing.T) {
	repo := &capturedSearch{}
	svc := &HistoryProviderService{messageRepo: repo}

	_, _, err := svc.SearchInboxEntries(conversation.SearchInboxInput{
		UserID:      "user-1",
		WorkspaceID: "ws-1",
		Personal:    personalinbox.ViewPinned,
		SortOrder:   conversation.SortOrderWaiting,
		LabelID:     "label-1",
	})

	require.NoError(t, err)
	require.Equal(t, personalinbox.ViewPinned, repo.input.Personal)
	require.Equal(t, "user-1", repo.input.PersonalUserID)
	require.Equal(t, conversation.SortOrderWaiting, repo.input.SortOrder)
	require.Equal(t, "label-1", repo.input.LabelID)
}
