package inbox_assignment_usecase

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vozko/domain/conversation"
	ce "vozko/domain/conversation_event"
	ia "vozko/domain/inbox_assignment"
)

type stubPortfolio struct {
	holder string
	err    error
	calls  int
}

func (s *stubPortfolio) HolderOfConversation(string, string, string) (string, error) {
	s.calls++
	return s.holder, s.err
}

var _ ia.PortfolioHolders = (*stubPortfolio)(nil)

func carteiraFixture(profile conversation.AutomationProfile, holder string, online ...string) (*aiFixture, *stubPortfolio) {
	f := newAIFixture(profile, online...)
	portfolio := &stubPortfolio{holder: holder}
	f.svc.SetPortfolio(portfolio)
	return f, portfolio
}

func inDepartment(f *aiFixture, departmentID string, online ...string) {
	f.svc.workspaceResolver = defaultResolver("ws-1", departmentID)
	f.eligible.departmentUsers["ws-1:"+departmentID] = online
}

func restrictedTo(departments ...string) conversation.DepartmentAccessScope {
	return conversation.DepartmentAccessScope{DepartmentIDs: departments, Restrict: true, WorkspaceHasDepartments: true}
}

func TestEnsureAssignment_CarteiraHolderGetsTheConversationEvenOffline(t *testing.T) {
	f, _ := carteiraFixture(noAutomation, "carla", "ana", "bob")

	got := f.svc.EnsureAssignment("entry-1", "whatsapp", "phone-1")

	assert.Equal(t, "carla", got, "the customer belongs to carla's carteira, online or not")
	assert.Equal(t, "carla", f.owner("entry-1"))
	assert.Equal(t, ia.TriggerPortfolio, f.lastHistory(t).Trigger)
	require.Len(t, f.events.logged, 1)
	assert.Equal(t, ce.EventAutoAssigned, f.events.logged[0].EventType)
	assert.Empty(t, f.repo.rrStates, "a carteira hand-out must not consume a roulette turn")
}

func TestEnsureAssignment_CarteiraWorksWithNobodyOnline(t *testing.T) {
	f, _ := carteiraFixture(noAutomation, "carla")

	assert.Equal(t, "carla", f.svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
}

func TestEnsureAssignment_AutomationAnswersBeforeTheCarteira(t *testing.T) {
	f, portfolio := carteiraFixture(agentGoverned, "carla", "ana")

	assert.Equal(t, "ai:agent-1", f.svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
	assert.Zero(t, portfolio.calls, "the agent answers first; the carteira is read when it hands off")
}

func TestEnsureAssignment_AnAssignedConversationIsNeverReRoutedToTheCarteira(t *testing.T) {
	f, portfolio := carteiraFixture(noAutomation, "carla", "ana")
	f.seed("entry-1", "bob")

	assert.Equal(t, "bob", f.svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
	assert.Zero(t, portfolio.calls)
}

func TestEnsureAssignment_NoCarteiraUsesTheRoulette(t *testing.T) {
	f, portfolio := carteiraFixture(noAutomation, "", "ana")

	assert.Equal(t, "ana", f.svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
	assert.Equal(t, ia.TriggerInboundRR, f.lastHistory(t).Trigger)
	assert.Equal(t, 1, portfolio.calls)
}

func TestEnsureAssignment_AHolderWhoCannotReceiveFallsBackToTheRoulette(t *testing.T) {
	cases := map[string]func(f *aiFixture){
		"left the workspace or lost conversation access": func(f *aiFixture) { f.receivers.denied["carla"] = true },
		"lost the carteira permission":                   func(f *aiFixture) { f.receivers.noPortfolio = map[string]bool{"carla": true} },
		"outside the conversation's department": func(f *aiFixture) {
			inDepartment(f, "dept-support", "ana")
			f.receivers.scopes = map[string]conversation.DepartmentAccessScope{"carla": restrictedTo("dept-sales")}
		},
	}
	for name, arrange := range cases {
		t.Run(name, func(t *testing.T) {
			f, _ := carteiraFixture(noAutomation, "carla", "ana")
			arrange(f)

			assert.Equal(t, "ana", f.svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
			assert.Equal(t, ia.TriggerInboundRR, f.lastHistory(t).Trigger)
		})
	}
}

func TestEnsureAssignment_AHolderInTheConversationsDepartmentGetsIt(t *testing.T) {
	f, _ := carteiraFixture(noAutomation, "carla")
	inDepartment(f, "dept-sales", "ana")
	f.receivers.scopes = map[string]conversation.DepartmentAccessScope{"carla": restrictedTo("dept-sales")}

	assert.Equal(t, "carla", f.svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
}

func TestEnsureAssignment_UnreadableCarteiraAssignsNobody(t *testing.T) {
	f, portfolio := carteiraFixture(noAutomation, "", "ana")
	portfolio.err = errors.New("db down")

	assert.Equal(t, "", f.svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"), "never deal a carteira customer to someone else")
	assert.Equal(t, "", f.owner("entry-1"))
	assert.Empty(t, f.repo.rrStates)
}

func TestHandOffToRoulette_AutomationHandsTheCarteiraCustomerToItsHolder(t *testing.T) {
	f, _ := carteiraFixture(agentGoverned, "carla", "ana", "bob")
	f.seed("entry-1", "ai:agent-1")

	got, err := f.svc.HandOffToRoulette(ia.RouletteHandOff{WorkspaceID: "ws-1", EntryID: "entry-1", EntryType: "whatsapp"})

	require.NoError(t, err)
	assert.Equal(t, "carla", got)
	assert.Equal(t, "carla", f.owner("entry-1"))
	h := f.lastHistory(t)
	assert.Equal(t, ia.TriggerAutomationHandoffPortfolio, h.Trigger)
	assert.Equal(t, "ai:agent-1", h.AssignedByActorID)
	assert.Equal(t, []string{"whatsapp:entry-1"}, f.pauser.paused, "the hand-off pauses the automation")
	assert.Equal(t, []string{"entry-1 from ai:agent-1"}, f.broadcast.owners)
	assert.Empty(t, f.repo.rrStates, "the ring keeps its turn")
}

func TestHandOffToRoulette_CarteiraHolderGetsItWithNobodyOnline(t *testing.T) {
	f, _ := carteiraFixture(agentGoverned, "carla")
	f.seed("entry-1", "ai:agent-1")

	got, err := f.svc.HandOffToRoulette(ia.RouletteHandOff{WorkspaceID: "ws-1", EntryID: "entry-1", EntryType: "whatsapp"})

	require.NoError(t, err)
	assert.Equal(t, "carla", got, "the carteira beats releasing it to the team")
}

func TestHandOffToRoulette_APersonHandingToADepartmentIsNotBouncedBack(t *testing.T) {
	f, portfolio := carteiraFixture(noAutomation, "carla", "ana")
	f.eligible.departmentUsers["ws-1:dept-sales"] = []string{"dan"}
	f.seed("entry-1", "carla")

	got, err := f.svc.HandOffToRoulette(ia.RouletteHandOff{WorkspaceID: "ws-1", EntryID: "entry-1", EntryType: "whatsapp", DepartmentID: "dept-sales", ByActorID: "carla"})

	require.NoError(t, err)
	assert.Equal(t, "dan", got)
	assert.Zero(t, portfolio.calls)
}

func TestHandOffToRoulette_AHolderOutsideTheTargetDepartmentLeavesItToThatDepartment(t *testing.T) {
	f, _ := carteiraFixture(agentGoverned, "carla")
	f.eligible.departmentUsers["ws-1:dept-sales"] = []string{"dan"}
	f.receivers.scopes = map[string]conversation.DepartmentAccessScope{"carla": restrictedTo("dept-support")}
	f.seed("entry-1", "ai:agent-1")

	got, err := f.svc.HandOffToRoulette(ia.RouletteHandOff{WorkspaceID: "ws-1", EntryID: "entry-1", EntryType: "whatsapp", DepartmentID: "dept-sales"})

	require.NoError(t, err)
	assert.Equal(t, "dan", got)
	assert.Equal(t, ia.TriggerAutomationHandoffRoulette, f.lastHistory(t).Trigger)
}

func TestHandOffToRoulette_UnreadableCarteiraKeepsTheConversationWithTheAutomation(t *testing.T) {
	f, portfolio := carteiraFixture(agentGoverned, "", "ana")
	portfolio.err = errors.New("db down")
	f.seed("entry-1", "ai:agent-1")

	_, err := f.svc.HandOffToRoulette(ia.RouletteHandOff{WorkspaceID: "ws-1", EntryID: "entry-1", EntryType: "whatsapp"})

	require.Error(t, err)
	assert.Equal(t, "ai:agent-1", f.owner("entry-1"))
	assert.Empty(t, f.pauser.paused)
}
