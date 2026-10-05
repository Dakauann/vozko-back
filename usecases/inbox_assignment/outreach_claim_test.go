package inbox_assignment_usecase

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	ia "vozko/domain/inbox_assignment"
)

func claimService(repo *mockRepo) (*AssignmentService, *recordingHistory) {
	history := &recordingHistory{}
	svc := outreachService(repo, &stubOutreach{userID: "u3"}, "u1", "u2", "u3")
	svc.SetHistory(history)
	return svc, history
}

func TestClaimTakesAFreeConversationForTheSender(t *testing.T) {
	repo := &mockRepo{}
	svc, history := claimService(repo)

	claimed, err := svc.ClaimIfUnassigned("entry-1", "whatsapp", "phone-1", "ws-1", "u2", ia.TriggerOutreachSent)

	require.NoError(t, err)
	require.True(t, claimed)
	require.Equal(t, "u2", repo.assigned.AssignedUserID)
	require.Len(t, history.appended, 1)
	require.Equal(t, ia.TriggerOutreachSent, history.appended[0].Trigger)
}

func TestClaimNeverTakesAnOwnedConversation(t *testing.T) {
	for _, owner := range []string{"u1", "ai:agent-1"} {
		repo := &mockRepo{findByEntryResult: &ia.InboxAssignment{AssignedUserID: owner}}
		svc, history := claimService(repo)

		claimed, err := svc.ClaimIfUnassigned("entry-1", "whatsapp", "phone-1", "ws-1", "u2", ia.TriggerOutreachSent)

		require.NoError(t, err)
		require.False(t, claimed, owner)
		require.Empty(t, repo.assignCalls, "the owner %s must keep the conversation", owner)
		require.Empty(t, history.appended)
	}
}

func TestClaimByTheCurrentOwnerIsANoOp(t *testing.T) {
	repo := &mockRepo{findByEntryResult: &ia.InboxAssignment{AssignedUserID: "u2"}}
	svc, history := claimService(repo)

	claimed, err := svc.ClaimIfUnassigned("entry-1", "whatsapp", "phone-1", "ws-1", "u2", ia.TriggerOutreachSent)

	require.NoError(t, err)
	require.False(t, claimed)
	require.Empty(t, repo.assignCalls)
	require.Empty(t, history.appended)
}

func TestOpeningAClaimedConversationKeepsTheSender(t *testing.T) {
	repo := &mockRepo{}
	svc, _ := claimService(repo)
	_, err := svc.ClaimIfUnassigned("entry-1", "whatsapp", "phone-1", "ws-1", "u2", ia.TriggerOutreachSent)
	require.NoError(t, err)

	opened, err := svc.AssignOnOpen("entry-1", "whatsapp", "phone-1", "ws-1", "u1")

	require.NoError(t, err)
	require.False(t, opened)
	require.Equal(t, "u2", repo.assigned.AssignedUserID)
}

func TestTheReplyToAClaimedConversationStaysWithTheSender(t *testing.T) {
	repo := &mockRepo{}
	svc, _ := claimService(repo)
	_, err := svc.ClaimIfUnassigned("entry-1", "whatsapp", "phone-1", "ws-1", "u2", ia.TriggerOutreachSent)
	require.NoError(t, err)

	require.Equal(t, "u2", svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
}

func TestAnOutreachClaimIsNeverRescued(t *testing.T) {
	require.False(t, slices.Contains(ia.RescueCandidateTriggers, ia.TriggerOutreachSent),
		"the sender chose this conversation, the rescue sweep must not hand it to someone else")
}
