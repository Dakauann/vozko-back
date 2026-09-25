package inbox_assignment_usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vozko/domain/actor"
	"vozko/domain/conversation"
	ce "vozko/domain/conversation_event"
	ia "vozko/domain/inbox_assignment"
	"vozko/domain/shared"
	dept "vozko/domain/workspace/workspace_department"
)

type stubAutomationProfiles struct {
	profile conversation.AutomationProfile
	err     error
	calls   int
}

func (s *stubAutomationProfiles) EntryAutomation(string, string) (conversation.AutomationProfile, error) {
	s.calls++
	return s.profile, s.err
}

type recordingBroadcaster struct {
	updated []string
	// owners records each reassignment as "<entry> from <previous owner>".
	owners []string
	// pausedWhenAnnounced is how many pauses had happened at each owner change,
	// to prove the card is rebuilt only once the automation stepped out.
	pausedWhenAnnounced []int
	pauser              *recordingPauser
}

func (b *recordingBroadcaster) BroadcastEntryUpdate(entryID, _ string, _ *conversation.Message) {
	b.updated = append(b.updated, entryID)
}

func (b *recordingBroadcaster) AnnounceOwnerChange(_, entryID, _, previousOwner string) {
	b.owners = append(b.owners, entryID+" from "+previousOwner)
	if b.pauser != nil {
		b.pausedWhenAnnounced = append(b.pausedWhenAnnounced, len(b.pauser.paused))
	}
}

type recordingPauser struct {
	paused []string
	err    error
}

func (p *recordingPauser) SetAutomation(_ context.Context, entryID string, entryType shared.EntryType, enabled *bool) error {
	if enabled == nil || *enabled {
		p.paused = append(p.paused, "resume:"+entryID)
		return p.err
	}
	p.paused = append(p.paused, string(entryType)+":"+entryID)
	return p.err
}

type recordingSessions struct {
	ended []string
}

func (s *recordingSessions) EndOpenRaw(workspaceID, entryID, entryType, outcome, reason, handoffUserID string) {
	s.ended = append(s.ended, workspaceID+"|"+entryID+"|"+outcome+"|"+reason+"|"+handoffUserID)
}

var agentGoverned = conversation.AutomationProfile{AgentID: "agent-1", AgentResponsesEnabled: true}

type aiFixture struct {
	svc       *AssignmentService
	repo      *statefulRepo
	profiles  *stubAutomationProfiles
	history   *recordingHistory
	events    *recordingEvents
	broadcast *recordingBroadcaster
	pauser    *recordingPauser
	sessions  *recordingSessions
	eligible  *mockEligible
	depts     *stubDepartments
	accounts  *stubAccounts
	receivers *stubReceivers
	config    *mockWorkspaceConfig
}

type stubDepartments struct {
	byID map[string]*dept.Department
}

func (s *stubDepartments) GetDepartmentByID(id string) (*dept.Department, error) {
	if d, ok := s.byID[id]; ok {
		return d, nil
	}
	return nil, errors.New("not found")
}

type stubAccounts struct {
	account string
	err     error
}

func (s *stubAccounts) EntryAccountID(string, string) (string, error) { return s.account, s.err }

type stubReceivers struct {
	denied map[string]bool
	// admins are the workspace's owners and admins; noRoulette lacks the
	// role permission to receive conversations.
	admins     map[string]bool
	noRoulette map[string]bool
}

func (s *stubReceivers) GetDepartmentScope(userID, _ string, _ bool) (conversation.DepartmentAccessScope, bool) {
	return conversation.DepartmentAccessScope{}, !s.denied[userID]
}

func (s *stubReceivers) HasWorkspacePermission(userID, _, resource, action string, _ bool) bool {
	if resource == ia.RouletteResource && action == ia.RouletteAction {
		return !s.noRoulette[userID]
	}
	return true
}

func (s *stubReceivers) IsWorkspaceOwnerOrAdmin(userID, _ string) bool { return s.admins[userID] }

func newAIFixture(profile conversation.AutomationProfile, humans ...string) *aiFixture {
	f := &aiFixture{
		repo:      newStatefulRepo(),
		profiles:  &stubAutomationProfiles{profile: profile},
		history:   &recordingHistory{},
		events:    &recordingEvents{},
		broadcast: &recordingBroadcaster{},
		pauser:    &recordingPauser{},
		sessions:  &recordingSessions{},
		eligible:  &mockEligible{workspaceUsers: humans, departmentUsers: map[string][]string{}},
		depts: &stubDepartments{byID: map[string]*dept.Department{
			"dept-sales": {ID: "dept-sales", WorkspaceID: "ws-1", Name: "Vendas"},
			"dept-other": {ID: "dept-other", WorkspaceID: "ws-2", Name: "Outro workspace"},
		}},
		accounts:  &stubAccounts{},
		receivers: &stubReceivers{denied: map[string]bool{}, admins: map[string]bool{}, noRoulette: map[string]bool{}},
		config:    defaultConfig(),
	}
	f.svc = newService(f.repo, f.eligible, defaultResolver("ws-1", ""), f.config)
	f.svc.SetDepartmentLookup(f.depts)
	f.svc.SetEntryAccountReader(f.accounts)
	f.svc.SetConversationReceivers(f.receivers)
	f.svc.SetAutomationGovernance(f.profiles)
	f.svc.SetHistory(f.history)
	f.svc.SetEventLogger(f.events)
	f.broadcast.pauser = f.pauser
	f.svc.SetEntryBroadcaster(f.broadcast)
	f.svc.SetAutomationPauser(f.pauser)
	f.svc.SetAISessionEnder(f.sessions)
	return f
}

func (f *aiFixture) owner(entryID string) string {
	a, _ := f.repo.FindByEntry("ws-1", entryID, "whatsapp")
	if a == nil {
		return ""
	}
	return a.AssignedUserID
}

func (f *aiFixture) seed(entryID, owner string) {
	_ = f.repo.Assign(&ia.InboxAssignment{
		WorkspaceID: "ws-1", BusinessPhoneID: "phone-1", EntryID: entryID, EntryType: "whatsapp", AssignedUserID: owner,
	})
}

func (f *aiFixture) lastHistory(t *testing.T) *ia.AssignmentHistory {
	t.Helper()
	require.NotEmpty(t, f.history.appended)
	return f.history.appended[len(f.history.appended)-1]
}

// --- the roulette ---

func TestEnsureAssignment_AIGovernedConversationGoesToTheAI(t *testing.T) {
	f := newAIFixture(agentGoverned, "ana", "bob")

	got := f.svc.EnsureAssignment("entry-1", "whatsapp", "phone-1")

	assert.Equal(t, "ai:agent-1", got)
	assert.Equal(t, "ai:agent-1", f.owner("entry-1"), "the AI must hold it, so no operator sees it")
	h := f.lastHistory(t)
	assert.Equal(t, ia.TriggerAutomationGoverned, h.Trigger)
	assert.Equal(t, string(actor.KindAI), h.ActorKind)
	require.Len(t, f.events.logged, 1)
	assert.Equal(t, ce.EventAutoAssigned, f.events.logged[0].EventType)
}

func TestEnsureAssignment_AIGovernedDoesNotMoveTheHumanRing(t *testing.T) {
	// Handing a conversation to the AI must not cost an operator their turn.
	f := newAIFixture(agentGoverned, "ana", "bob")

	f.svc.EnsureAssignment("entry-1", "whatsapp", "phone-1")

	assert.Empty(t, f.repo.rrStates, "the AI is not a member of the ring")
}

func TestEnsureAssignment_WorkflowGovernedGoesToTheWorkflow(t *testing.T) {
	f := newAIFixture(conversation.AutomationProfile{WorkflowID: "wf-1", WorkflowEnabled: true}, "ana")

	assert.Equal(t, "workflow:wf-1", f.svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
}

func TestEnsureAssignment_AIGovernedEvenWithNobodyOnline(t *testing.T) {
	// The AI answers regardless of who is connected; an empty ring is irrelevant.
	f := newAIFixture(agentGoverned)

	assert.Equal(t, "ai:agent-1", f.svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
}

func TestEnsureAssignment_PausedAIFallsBackToTheHumanRoulette(t *testing.T) {
	paused := false
	f := newAIFixture(conversation.AutomationProfile{AgentID: "agent-1", AgentResponsesEnabled: true, AutomationEnabled: &paused}, "ana")

	assert.Equal(t, "ana", f.svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
}

func TestEnsureAssignment_NoAIUsesTheHumanRoulette(t *testing.T) {
	f := newAIFixture(conversation.AutomationProfile{}, "ana")

	assert.Equal(t, "ana", f.svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
}

func TestEnsureAssignment_ProfileErrorAssignsNobody(t *testing.T) {
	// Not knowing whether the AI governs must not be read as "it does not":
	// that would hand an AI conversation to an operator. Nothing is written and
	// the next inbound message decides again.
	f := newAIFixture(conversation.AutomationProfile{}, "ana")
	f.profiles.err = errors.New("db down")

	assert.Equal(t, "", f.svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
	assert.Equal(t, "", f.owner("entry-1"))
	assert.Empty(t, f.repo.rrStates)
}

func TestEnsureAssignment_AlreadyHeldIsNotReconsidered(t *testing.T) {
	// A human the AI handed off to keeps the conversation on the next message.
	f := newAIFixture(agentGoverned, "ana")
	f.seed("entry-1", "bob")

	assert.Equal(t, "bob", f.svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
	assert.Equal(t, 0, f.profiles.calls, "an owned conversation needs no governance read")
}

func TestEnsureAssignment_WithoutAReaderKeepsTodaysBehaviour(t *testing.T) {
	svc := newService(newStatefulRepo(), defaultEligible("ana"), defaultResolver("ws-1", ""), defaultConfig())

	assert.Equal(t, "ana", svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
}

// --- hand-off to a named human ---

func TestHandOffToHuman_MovesTheConversationAndCreditsTheAI(t *testing.T) {
	f := newAIFixture(agentGoverned, "ana")
	f.seed("entry-1", "ai:agent-1")

	require.NoError(t, f.svc.HandOffToHuman("ws-1", "entry-1", "whatsapp", "bob"))

	assert.Equal(t, "bob", f.owner("entry-1"))
	h := f.lastHistory(t)
	assert.Equal(t, ia.TriggerAutomationHandoff, h.Trigger)
	assert.Equal(t, "ai:agent-1", h.AssignedByActorID, "the AI made the transfer")
	assert.Equal(t, "ai:agent-1", h.PreviousActorID)
	assert.Equal(t, []string{"entry-1 from ai:agent-1"}, f.broadcast.owners, "the new owner's inbox must receive it live")
	assert.Equal(t, []int{1}, f.broadcast.pausedWhenAnnounced, "announced once, after the automation stepped out")
	assert.Empty(t, f.broadcast.updated)
}

func TestHandOffToHuman_FromAnUnheldConversationIsCreditedToTheSystem(t *testing.T) {
	f := newAIFixture(agentGoverned)

	require.NoError(t, f.svc.HandOffToHuman("ws-1", "entry-1", "whatsapp", "bob"))

	assert.Equal(t, "bob", f.owner("entry-1"))
	assert.Equal(t, actor.SystemID, f.lastHistory(t).AssignedByActorID)
}

func TestHandOffToHuman_RefusesAnAITargetOrNoTarget(t *testing.T) {
	f := newAIFixture(agentGoverned)
	f.seed("entry-1", "ai:agent-1")

	assert.ErrorIs(t, f.svc.HandOffToHuman("ws-1", "entry-1", "whatsapp", "ai:agent-2"), ErrHandOffTargetNotHuman)
	assert.ErrorIs(t, f.svc.HandOffToHuman("ws-1", "entry-1", "whatsapp", "  "), ErrHandOffTargetNotHuman)
	assert.Equal(t, "ai:agent-1", f.owner("entry-1"))
	assert.Empty(t, f.broadcast.updated)
	assert.Empty(t, f.broadcast.owners)
}

// --- hand-off through the roulette ---

func TestHandOffToRoulette_PicksTheNextHumanInTheRing(t *testing.T) {
	f := newAIFixture(agentGoverned, "ana", "bob")
	f.seed("entry-1", "ai:agent-1")

	got, err := f.svc.HandOffToRoulette(ia.RouletteHandOff{WorkspaceID: "ws-1", EntryID: "entry-1", EntryType: "whatsapp"})

	require.NoError(t, err)
	assert.Equal(t, "ana", got)
	assert.Equal(t, "ana", f.owner("entry-1"))
	assert.Equal(t, ia.TriggerAutomationHandoffRoulette, f.lastHistory(t).Trigger, "a roulette hand-out the rescue sweep can pass on")
	assert.Equal(t, "ai:agent-1", f.lastHistory(t).AssignedByActorID)
	assert.Equal(t, []string{"entry-1 from ai:agent-1"}, f.broadcast.owners)

	f.seed("entry-2", "ai:agent-1")
	next, err := f.svc.HandOffToRoulette(ia.RouletteHandOff{WorkspaceID: "ws-1", EntryID: "entry-2", EntryType: "whatsapp"})
	require.NoError(t, err)
	assert.Equal(t, "bob", next, "hand-offs share the ring with inbound conversations")
}

func TestHandOffToRoulette_NobodyEligibleReleasesToTheTeam(t *testing.T) {
	// Hidden and unanswered is the one state that must not happen: with nobody
	// to take it, the conversation becomes the whole team's queue.
	f := newAIFixture(agentGoverned)
	f.seed("entry-1", "ai:agent-1")

	got, err := f.svc.HandOffToRoulette(ia.RouletteHandOff{WorkspaceID: "ws-1", EntryID: "entry-1", EntryType: "whatsapp"})

	require.NoError(t, err)
	assert.Equal(t, "", got)
	assert.Equal(t, "", f.owner("entry-1"))
	assert.Equal(t, []string{"entry-1 from ai:agent-1"}, f.broadcast.owners, "the whole team must see it arrive in the queue")
	assert.Equal(t, []int{1}, f.broadcast.pausedWhenAnnounced)
}

func TestHandOffToRoulette_AHumanOwnerIsKept(t *testing.T) {
	f := newAIFixture(agentGoverned, "ana")
	f.seed("entry-1", "bob")

	got, err := f.svc.HandOffToRoulette(ia.RouletteHandOff{WorkspaceID: "ws-1", EntryID: "entry-1", EntryType: "whatsapp"})

	require.NoError(t, err)
	assert.Equal(t, "bob", got)
	assert.Equal(t, "bob", f.owner("entry-1"))
	assert.Empty(t, f.broadcast.owners, "nobody gained or lost it")
	assert.Equal(t, []string{"entry-1"}, f.broadcast.updated, "the card must show the automation paused")
}

func TestHandOffToRoulette_UnheldConversationStaysWithTheTeam(t *testing.T) {
	// No row means no roulette context (business phone) and the team already sees it.
	f := newAIFixture(agentGoverned, "ana")

	got, err := f.svc.HandOffToRoulette(ia.RouletteHandOff{WorkspaceID: "ws-1", EntryID: "entry-1", EntryType: "whatsapp"})

	require.NoError(t, err)
	assert.Equal(t, "", got)
	assert.Equal(t, "", f.owner("entry-1"))
	assert.Empty(t, f.broadcast.owners)
	assert.Equal(t, []string{"entry-1"}, f.broadcast.updated, "the card must show the automation paused")
}

// --- taking over on pause ---

// Whoever stops the agent or workflow is about to answer, so the conversation
// becomes theirs, credited to them on the timeline.
func TestTakeOverFromAutomation_GivesItToWhoeverPaused(t *testing.T) {
	for _, holder := range []string{"ai:agent-1", "workflow:wf-1", ""} {
		t.Run("held by "+holder, func(t *testing.T) {
			f := newAIFixture(agentGoverned)
			if holder != "" {
				f.seed("entry-1", holder)
			}

			owner, err := f.svc.TakeOverFromAutomation("entry-1", "whatsapp", "carla")

			require.NoError(t, err)
			assert.Equal(t, "carla", owner)
			assert.Equal(t, "carla", f.owner("entry-1"))
			h := f.lastHistory(t)
			assert.Equal(t, ia.TriggerAutomationTakenOver, h.Trigger)
			assert.Equal(t, "carla", h.AssignedByActorID)
			assert.Equal(t, []string{"entry-1 from " + holder}, f.broadcast.owners)
		})
	}
}

func TestTakeOverFromAutomation_SomeoneWhoCannotReceiveLeavesItToTheTeam(t *testing.T) {
	f := newAIFixture(agentGoverned)
	f.seed("entry-1", "ai:agent-1")
	f.receivers.denied["carla"] = true

	owner, err := f.svc.TakeOverFromAutomation("entry-1", "whatsapp", "carla")

	require.NoError(t, err)
	assert.Equal(t, "", owner)
	assert.Equal(t, "", f.owner("entry-1"), "a paused automation must not keep it hidden")
	require.NotEmpty(t, f.events.logged)
	assert.Equal(t, ce.EventUnassigned, f.events.logged[len(f.events.logged)-1].EventType)
	assert.Equal(t, []string{"entry-1 from ai:agent-1"}, f.broadcast.owners, "the team must see it arrive")
}

// Taking over follows the workspace rule for who receives conversations, the
// same one opening an unowned conversation follows: with admins left out of
// the roulette, an admin who pauses leaves it to the team queue.
func TestTakeOverFromAutomation_FollowsTheWorkspaceRuleForAdmins(t *testing.T) {
	for _, tc := range []struct {
		name       string
		skipAdmins bool
		want       string
	}{
		{"admins left out", true, ""},
		{"admins receive", false, "carla"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAIFixture(agentGoverned)
			f.seed("entry-1", "ai:agent-1")
			f.receivers.admins["carla"] = true
			f.config.skipAdmins = tc.skipAdmins

			owner, err := f.svc.TakeOverFromAutomation("entry-1", "whatsapp", "carla")

			require.NoError(t, err)
			assert.Equal(t, tc.want, owner)
			assert.Equal(t, tc.want, f.owner("entry-1"))
		})
	}
}

func TestTakeOverFromAutomation_SomeoneWhoseRoleReceivesNothingLeavesItToTheTeam(t *testing.T) {
	f := newAIFixture(agentGoverned)
	f.seed("entry-1", "ai:agent-1")
	f.receivers.noRoulette["carla"] = true

	owner, err := f.svc.TakeOverFromAutomation("entry-1", "whatsapp", "carla")

	require.NoError(t, err)
	assert.Equal(t, "", owner)
	assert.Equal(t, "", f.owner("entry-1"))
}

func TestTakeOverFromAutomation_LeavesAColleaguesConversationAlone(t *testing.T) {
	f := newAIFixture(agentGoverned)
	f.seed("entry-1", "bob")

	owner, err := f.svc.TakeOverFromAutomation("entry-1", "whatsapp", "carla")

	require.NoError(t, err)
	assert.Equal(t, "bob", owner)
	assert.Equal(t, "bob", f.owner("entry-1"))
	assert.Empty(t, f.broadcast.owners)
}

func TestTakeOverFromAutomation_UnresolvableWorkspaceIsAnError(t *testing.T) {
	f := newAIFixture(agentGoverned)
	f.svc.workspaceResolver = &mockResolver{workspaceErr: errors.New("gone")}

	_, err := f.svc.TakeOverFromAutomation("entry-1", "whatsapp", "carla")
	assert.Error(t, err)
}

// --- the AI steps out on every hand-off ---

func TestEveryHandOffPausesTheAIForThatConversation(t *testing.T) {
	// Reply paths and workflow triggers honour only the pause switch, never the
	// assignee. Without the pause the agent keeps replying, and a workflow keeps
	// starting runs, while the person it was handed to is attending.
	cases := map[string]func(f *aiFixture) error{
		"named human (workflow nodes)": func(f *aiFixture) error {
			return f.svc.HandOffToHuman("ws-1", "entry-1", "whatsapp", "bob")
		},
		"roulette (agent tool)": func(f *aiFixture) error {
			_, err := f.svc.HandOffToRoulette(ia.RouletteHandOff{WorkspaceID: "ws-1", EntryID: "entry-1", EntryType: "whatsapp"})
			return err
		},
	}
	for name, handOff := range cases {
		t.Run(name, func(t *testing.T) {
			f := newAIFixture(agentGoverned, "ana")
			f.seed("entry-1", "ai:agent-1")

			require.NoError(t, handOff(f))

			assert.Equal(t, []string{"whatsapp:entry-1"}, f.pauser.paused)
			assert.False(t, f.repo.assignments[assignmentKey("ws-1", "entry-1", "whatsapp")].HeldByAutomation())
		})
	}
}

func TestReleasingToTheTeamAlsoPausesTheAI(t *testing.T) {
	f := newAIFixture(agentGoverned)
	f.seed("entry-1", "ai:agent-1")

	_, err := f.svc.HandOffToRoulette(ia.RouletteHandOff{WorkspaceID: "ws-1", EntryID: "entry-1", EntryType: "whatsapp"})

	require.NoError(t, err)
	assert.Equal(t, []string{"whatsapp:entry-1"}, f.pauser.paused)
}

func TestAPauseFailureAfterTheHandOffIsReported(t *testing.T) {
	// The person has it, but the AI may still answer: the caller must know.
	f := newAIFixture(agentGoverned, "ana")
	f.seed("entry-1", "ai:agent-1")
	f.pauser.err = errors.New("db down")

	err := f.svc.HandOffToHuman("ws-1", "entry-1", "whatsapp", "bob")

	assert.ErrorIs(t, err, ErrAutomationStillActive)
	assert.Equal(t, "bob", f.owner("entry-1"), "the hand-off itself stands")
}

func TestAFailedHandOffDoesNotPause(t *testing.T) {
	// If nobody took it, pausing would leave the contact with no one at all.
	f := newAIFixture(agentGoverned)
	f.seed("entry-1", "ai:agent-1")

	require.Error(t, f.svc.HandOffToHuman("ws-1", "entry-1", "whatsapp", "ai:agent-2"))
	assert.Empty(t, f.pauser.paused)
}

// --- a workflow is its own actor ---

func TestWorkflowHandOffIsCreditedToTheWorkflow(t *testing.T) {
	// The workflow transfer nodes hand off what the workflow held; history and
	// the timeline must name the workflow, not an AI agent.
	f := newAIFixture(conversation.AutomationProfile{WorkflowID: "wf-1", WorkflowEnabled: true}, "ana")
	f.seed("entry-1", "workflow:wf-1")

	require.NoError(t, f.svc.HandOffToHuman("ws-1", "entry-1", "whatsapp", "bob"))

	h := f.lastHistory(t)
	assert.Equal(t, "workflow:wf-1", h.AssignedByActorID)
	assert.Equal(t, "workflow:wf-1", h.PreviousActorID)
	assert.Equal(t, string(actor.KindHuman), h.ActorKind)
	last := f.events.logged[len(f.events.logged)-1]
	assert.Equal(t, actor.KindWorkflow, last.ActorKind)
	assert.Equal(t, []string{"whatsapp:entry-1"}, f.pauser.paused, "the workflow steps out like an agent does")
}

func TestWorkflowGovernedHistoryCarriesTheWorkflowKind(t *testing.T) {
	f := newAIFixture(conversation.AutomationProfile{WorkflowID: "wf-1", WorkflowEnabled: true})

	f.svc.EnsureAssignment("entry-1", "whatsapp", "phone-1")

	assert.Equal(t, string(actor.KindWorkflow), f.lastHistory(t).ActorKind)
}

func TestAHandOffCannotTargetAWorkflow(t *testing.T) {
	f := newAIFixture(agentGoverned)
	f.seed("entry-1", "ai:agent-1")

	assert.ErrorIs(t, f.svc.HandOffToHuman("ws-1", "entry-1", "whatsapp", "workflow:wf-1"), ErrHandOffTargetNotHuman)
}

// --- handing a conversation back to its automation ---

var (
	workflowGoverned = conversation.AutomationProfile{WorkflowID: "wf-1", WorkflowEnabled: true}
	noAutomation     = conversation.AutomationProfile{}
)

func TestReturnToAutomation_GivesItToTheGoverningAutomation(t *testing.T) {
	f := newAIFixture(agentGoverned)
	f.seed("entry-1", "bob")

	owner, err := f.svc.ReturnToAutomation("entry-1", "whatsapp", "carla")

	require.NoError(t, err)
	assert.Equal(t, "ai:agent-1", owner)
	assert.Equal(t, "ai:agent-1", f.owner("entry-1"))
	h := f.lastHistory(t)
	assert.Equal(t, ia.TriggerAutomationResumed, h.Trigger)
	assert.Equal(t, "carla", h.AssignedByActorID, "credited to whoever handed it back")
	assert.Equal(t, "bob", h.PreviousActorID)
	assert.Equal(t, string(actor.KindAI), h.ActorKind)
	assert.Equal(t, []string{"entry-1 from bob"}, f.broadcast.owners, "bob must lose it at once")
}

func TestReturnToAutomation_GoesToWhoeverAnswersNowNotWhoHandedOff(t *testing.T) {
	// The channel now runs a workflow; replies follow the channel, so the
	// owner must too, or the screen would name one actor while another answers.
	f := newAIFixture(workflowGoverned)
	f.seed("entry-1", "bob")

	owner, err := f.svc.ReturnToAutomation("entry-1", "whatsapp", "bob")

	require.NoError(t, err)
	assert.Equal(t, "workflow:wf-1", owner)
}

func TestReturnToAutomation_AnUnassignedConversationIsTakenNow(t *testing.T) {
	f := newAIFixture(agentGoverned)

	owner, err := f.svc.ReturnToAutomation("entry-1", "whatsapp", "carla")

	require.NoError(t, err)
	assert.Equal(t, "ai:agent-1", owner)
	assert.Equal(t, "ai:agent-1", f.owner("entry-1"))
}

func TestReturnToAutomation_AlreadyHeldIsANoOp(t *testing.T) {
	f := newAIFixture(agentGoverned)
	f.seed("entry-1", "ai:agent-1")

	owner, err := f.svc.ReturnToAutomation("entry-1", "whatsapp", "carla")

	require.NoError(t, err)
	assert.Equal(t, "ai:agent-1", owner)
	assert.Empty(t, f.history.appended)
	assert.Empty(t, f.broadcast.updated)
}

func TestReturnToAutomation_RefusesWhenNothingWouldAnswer(t *testing.T) {
	// Handing back with no agent or workflow configured (or paused) would give
	// the contact to nobody.
	paused := false
	for name, profile := range map[string]conversation.AutomationProfile{
		"nothing configured": noAutomation,
		"paused":             {AgentID: "agent-1", AgentResponsesEnabled: true, AutomationEnabled: &paused},
	} {
		t.Run(name, func(t *testing.T) {
			f := newAIFixture(profile)
			f.seed("entry-1", "bob")

			owner, err := f.svc.ReturnToAutomation("entry-1", "whatsapp", "carla")

			assert.ErrorIs(t, err, ErrNothingToReturnTo)
			assert.Equal(t, "bob", owner, "the current owner is reported unchanged")
			assert.Equal(t, "bob", f.owner("entry-1"))
		})
	}
}

func TestReturnToAutomation_AnUnreadableProfileChangesNothing(t *testing.T) {
	f := newAIFixture(agentGoverned)
	f.seed("entry-1", "bob")
	f.profiles.err = errors.New("db down")

	_, err := f.svc.ReturnToAutomation("entry-1", "whatsapp", "carla")

	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrNothingToReturnTo, "a read failure is not 'nothing configured'")
	assert.Equal(t, "bob", f.owner("entry-1"))
}

// --- the AI session ends at the hand-off ---

func TestHandOffsEndTheAISessionAsHandedOff(t *testing.T) {
	// Finishing a conversation closes any open AI session as "contained". If the
	// session outlived the hand-off, a person closing without replying would be
	// counted as the AI resolving it.
	t.Run("to a named person", func(t *testing.T) {
		f := newAIFixture(agentGoverned)
		f.seed("entry-1", "ai:agent-1")

		require.NoError(t, f.svc.HandOffToHuman("ws-1", "entry-1", "whatsapp", "bob"))

		assert.Equal(t, []string{"ws-1|entry-1|handed_off|automation_handoff|bob"}, f.sessions.ended)
	})
	t.Run("through the roulette", func(t *testing.T) {
		f := newAIFixture(agentGoverned, "ana")
		f.seed("entry-1", "ai:agent-1")

		_, err := f.svc.HandOffToRoulette(ia.RouletteHandOff{WorkspaceID: "ws-1", EntryID: "entry-1", EntryType: "whatsapp"})

		require.NoError(t, err)
		assert.Equal(t, []string{"ws-1|entry-1|handed_off|automation_handoff|ana"}, f.sessions.ended)
	})
	t.Run("to the team queue", func(t *testing.T) {
		f := newAIFixture(agentGoverned)
		f.seed("entry-1", "ai:agent-1")

		_, err := f.svc.HandOffToRoulette(ia.RouletteHandOff{WorkspaceID: "ws-1", EntryID: "entry-1", EntryType: "whatsapp"})

		require.NoError(t, err)
		assert.Equal(t, []string{"ws-1|entry-1|handed_off|automation_handoff|"}, f.sessions.ended)
	})
}

func TestAFailedHandOffLeavesTheSessionOpen(t *testing.T) {
	f := newAIFixture(agentGoverned)
	f.seed("entry-1", "ai:agent-1")

	require.Error(t, f.svc.HandOffToHuman("ws-1", "entry-1", "whatsapp", "workflow:wf-1"))

	assert.Empty(t, f.sessions.ended, "the AI still has the conversation")
}

// --- the roulette, for any automation and any department ---

func departmentHandOff(departmentID string) ia.RouletteHandOff {
	return ia.RouletteHandOff{WorkspaceID: "ws-1", EntryID: "entry-1", EntryType: "whatsapp", DepartmentID: departmentID, ByActorID: "workflow:wf-1"}
}

func TestHandOffToRoulette_DrawsFromTheChosenDepartmentsRing(t *testing.T) {
	// Same ring the first customer message uses, keyed by that department:
	// the workspace's mode, its eligibility and its shared pointer all apply.
	f := newAIFixture(workflowGoverned, "ana")
	f.eligible.departmentUsers["ws-1:dept-sales"] = []string{"carla", "davi"}
	f.seed("entry-1", "workflow:wf-1")

	got, err := f.svc.HandOffToRoulette(departmentHandOff("dept-sales"))

	require.NoError(t, err)
	assert.Equal(t, "carla", got)
	assert.Equal(t, "carla", f.owner("entry-1"))
	require.NotEmpty(t, f.eligible.departmentCalls)
	assert.Equal(t, "dept-sales", f.eligible.departmentCalls[len(f.eligible.departmentCalls)-1].departmentID)
	assert.Equal(t, "carla", f.repo.rrStates[rrKey("ws-1", "phone-1", "dept-sales")].LastAssignedUserID,
		"the department's own pointer moves, shared with its inbound conversations")
	h := f.lastHistory(t)
	assert.Equal(t, ia.TriggerAutomationHandoffRoulette, h.Trigger)
	assert.Equal(t, "workflow:wf-1", h.AssignedByActorID)
}

func TestHandOffToRoulette_RefusesADepartmentOutsideTheWorkspace(t *testing.T) {
	// A workflow variable could produce any id; the hand-off must never deal a
	// customer to people in another workspace.
	for _, id := range []string{"dept-other", "dept-missing"} {
		t.Run(id, func(t *testing.T) {
			f := newAIFixture(workflowGoverned, "ana")
			f.eligible.departmentUsers["ws-2:"+id] = []string{"intruder"}
			f.seed("entry-1", "workflow:wf-1")

			_, err := f.svc.HandOffToRoulette(departmentHandOff(id))

			assert.ErrorIs(t, err, ia.ErrDepartmentOutOfScope)
			assert.Equal(t, "workflow:wf-1", f.owner("entry-1"))
			assert.Empty(t, f.pauser.paused)
			assert.Empty(t, f.sessions.ended)
		})
	}
}

func TestHandOffToRoulette_WithoutADepartmentLookupRefusesAChosenDepartment(t *testing.T) {
	f := newAIFixture(workflowGoverned, "ana")
	f.svc.SetDepartmentLookup(nil)
	f.seed("entry-1", "workflow:wf-1")

	_, err := f.svc.HandOffToRoulette(departmentHandOff("dept-sales"))

	assert.ErrorIs(t, err, ia.ErrDepartmentOutOfScope)
}

func TestHandOffToRoulette_AChosenDepartmentReplacesAPerson(t *testing.T) {
	// "Transfer to Sales" means Sales, even if someone else holds it now; only
	// the default (the conversation's own department) keeps a person in place.
	f := newAIFixture(workflowGoverned)
	f.eligible.departmentUsers["ws-1:dept-sales"] = []string{"carla"}
	f.seed("entry-1", "bob")

	got, err := f.svc.HandOffToRoulette(departmentHandOff("dept-sales"))

	require.NoError(t, err)
	assert.Equal(t, "carla", got)
	assert.Equal(t, "bob", f.lastHistory(t).PreviousActorID)
}

func TestHandOffToRoulette_AnUnassignedConversationIsDealtWithItsAccount(t *testing.T) {
	// No row means no business phone on record; the channel's account keys the
	// ring instead of leaving the conversation in the queue.
	f := newAIFixture(workflowGoverned, "ana")
	f.accounts.account = "phone-9"

	got, err := f.svc.HandOffToRoulette(ia.RouletteHandOff{WorkspaceID: "ws-1", EntryID: "entry-1", EntryType: "whatsapp"})

	require.NoError(t, err)
	assert.Equal(t, "ana", got)
	assert.Equal(t, "ana", f.repo.rrStates[rrKey("ws-1", "phone-9", "")].LastAssignedUserID)
}

func TestHandOffToRoulette_AnEmptyDepartmentRingGoesToTheTeamQueue(t *testing.T) {
	f := newAIFixture(workflowGoverned, "ana")
	f.seed("entry-1", "workflow:wf-1")

	got, err := f.svc.HandOffToRoulette(departmentHandOff("dept-sales"))

	require.NoError(t, err)
	assert.Equal(t, "", got)
	assert.Equal(t, "", f.owner("entry-1"))
}

// --- a named person must be able to answer ---

func TestHandOffToHuman_RefusesSomeoneWithoutConversationAccess(t *testing.T) {
	// The same check the manual "assign to" makes: a member without
	// conversation access would receive a conversation nobody can answer.
	f := newAIFixture(workflowGoverned)
	f.seed("entry-1", "workflow:wf-1")
	f.receivers.denied["guest"] = true

	err := f.svc.HandOffToHuman("ws-1", "entry-1", "whatsapp", "guest")

	assert.ErrorIs(t, err, ia.ErrHandOffTargetNoAccess)
	assert.Equal(t, "workflow:wf-1", f.owner("entry-1"))
	assert.Empty(t, f.pauser.paused)
}

func TestHandOffToHuman_WithoutAnAccessCheckRefusesEveryone(t *testing.T) {
	f := newAIFixture(workflowGoverned)
	f.svc.SetConversationReceivers(nil)

	assert.ErrorIs(t, f.svc.HandOffToHuman("ws-1", "entry-1", "whatsapp", "bob"), ia.ErrHandOffTargetNoAccess)
}

// Every reassignment, whoever makes it (a person, the rescue sweep, a bulk
// action, an incoming call), is announced with the owner it had, so the one who
// lost it drops it and the one who got it sees it.
func TestEveryReassignmentIsAnnouncedWithItsPreviousOwner(t *testing.T) {
	f := newAIFixture(agentGoverned)
	f.seed("entry-1", "ana")

	require.NoError(t, f.svc.AssignManual("entry-1", "whatsapp", "", "ws-1", "bob", "carla", ia.TriggerRescue))
	require.NoError(t, f.svc.UnassignSystem("entry-1", "whatsapp", "ws-1", ia.TriggerManual))
	require.NoError(t, f.svc.AssignManual("entry-1", "whatsapp", "", "ws-1", "dora", "carla", ia.TriggerManual))

	assert.Equal(t, []string{"entry-1 from ana", "entry-1 from bob", "entry-1 from "}, f.broadcast.owners)
}

func TestAnUnchangedOwnerIsNotAnnounced(t *testing.T) {
	f := newAIFixture(agentGoverned)
	f.seed("entry-1", "ana")

	require.NoError(t, f.svc.AssignManual("entry-1", "whatsapp", "", "ws-1", "ana", "carla", ia.TriggerManual))
	require.NoError(t, f.svc.UnassignSystem("entry-2", "whatsapp", "ws-1", ia.TriggerManual))

	assert.Empty(t, f.broadcast.owners)
}
