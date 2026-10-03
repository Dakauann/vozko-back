package inbox_assignment_usecase

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	ia "vozko/domain/inbox_assignment"
)

type stubOutreach struct {
	userID string
	err    error
	calls  int
}

func (s *stubOutreach) LastOutreachBy(string, string) (string, error) {
	s.calls++
	return s.userID, s.err
}

var _ ia.EntryOutreachReader = (*stubOutreach)(nil)

func outreachService(repo *mockRepo, outreach ia.EntryOutreachReader, users ...string) *AssignmentService {
	svc := newService(repo, defaultEligible(users...), defaultResolver("ws-1", ""), defaultConfig())
	if outreach != nil {
		svc.SetOutreach(outreach)
	}
	return svc
}

func TestEnsureAssignment_OutreachOwnerKeepsTheLead(t *testing.T) {
	repo := &mockRepo{}
	svc := outreachService(repo, &stubOutreach{userID: "u2"}, "u1", "u2", "u3")

	got := svc.EnsureAssignment("entry-1", "whatsapp", "phone-1")

	require.Equal(t, "u2", got, "the lead must go back to the operator who opened the conversation")
	require.NotNil(t, repo.assigned)
	require.Equal(t, "u2", repo.assigned.AssignedUserID)
}

func TestEnsureAssignment_OutreachDoesNotAdvanceTheRoulette(t *testing.T) {
	repo := &mockRepo{}
	svc := outreachService(repo, &stubOutreach{userID: "u3"}, "u1", "u2", "u3")

	require.Equal(t, "u3", svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
	require.Empty(t, repo.savedStates, "a sticky assignment must not consume a round-robin turn")
}

func TestEnsureAssignment_OutreachOwnerNotEligible_FallsBackToRoulette(t *testing.T) {
	repo := &mockRepo{}
	svc := outreachService(repo, &stubOutreach{userID: "u9"}, "u1", "u2", "u3")

	got := svc.EnsureAssignment("entry-1", "whatsapp", "phone-1")

	require.Equal(t, "u1", got, "an offline or ineligible outreacher must not strand the lead")
	require.Len(t, repo.savedStates, 1)
}

func TestEnsureAssignment_NoOutreach_StillUsesRoulette(t *testing.T) {
	repo := &mockRepo{}
	svc := outreachService(repo, &stubOutreach{userID: ""}, "u1", "u2", "u3")

	require.Equal(t, "u1", svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
}

func TestEnsureAssignment_OutreachReaderUnset_StillUsesRoulette(t *testing.T) {
	repo := &mockRepo{}
	svc := outreachService(repo, nil, "u1", "u2", "u3")

	require.Equal(t, "u1", svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
}

func TestEnsureAssignment_OutreachReaderError_FallsBackToRoulette(t *testing.T) {
	repo := &mockRepo{}
	out := &stubOutreach{err: errors.New("db down")}
	svc := outreachService(repo, out, "u1", "u2", "u3")

	require.Equal(t, "u1", svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
	require.Equal(t, 1, out.calls)
}

func TestEnsureAssignment_AlreadyAssigned_OutreachIgnored(t *testing.T) {
	repo := &mockRepo{findByEntryResult: &ia.InboxAssignment{AssignedUserID: "u1"}}
	out := &stubOutreach{userID: "u2"}
	svc := outreachService(repo, out, "u1", "u2", "u3")

	require.Equal(t, "u1", svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
	require.Zero(t, out.calls, "an assigned conversation must never be re-routed")
}

func TestEnsureAssignment_OutreachRecordsItsOwnTrigger(t *testing.T) {
	repo := &mockRepo{}
	history := &recordingHistory{}
	svc := outreachService(repo, &stubOutreach{userID: "u2"}, "u1", "u2", "u3")
	svc.SetHistory(history)

	require.Equal(t, "u2", svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
	require.Len(t, history.appended, 1)
	require.Equal(t, ia.TriggerOutreach, history.appended[0].Trigger)
}

func TestEnsureAssignment_RouletteKeepsItsTrigger(t *testing.T) {
	repo := &mockRepo{}
	history := &recordingHistory{}
	svc := outreachService(repo, &stubOutreach{userID: ""}, "u1", "u2", "u3")
	svc.SetHistory(history)

	require.Equal(t, "u1", svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
	require.Len(t, history.appended, 1)
	require.Equal(t, ia.TriggerInboundRR, history.appended[0].Trigger)
}
