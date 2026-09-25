package inbox_assignment_usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vozko/domain/shared"
)

func pauseOff() *bool { off := false; return &off }
func pauseOn() *bool  { on := true; return &on }

type stubAccess struct {
	allowed bool
	asked   []string
}

func (a *stubAccess) CanAccessEntry(userID, workspaceID, entryID, entryType string, isAdmin bool) bool {
	a.asked = append(a.asked, userID+"|"+workspaceID+"|"+entryID+"|"+entryType)
	return a.allowed
}

func toggleInput(enabled *bool) OperatorAutomationInput {
	return OperatorAutomationInput{
		ActorUserID: "bob",
		WorkspaceID: "ws-1",
		EntryID:     "entry-1",
		EntryType:   shared.EntryTypeWhatsApp,
		Enabled:     enabled,
	}
}

func newToggle(f *aiFixture) (*OperatorAutomationToggle, *stubAccess) {
	access := &stubAccess{allowed: true}
	return NewOperatorAutomationToggle(f.pauser, f.svc, access), access
}

// --- access ---

func TestOperatorToggle_RefusesAConversationTheCallerCannotAccess(t *testing.T) {
	// Switching automation moves ownership now, so the route permission alone
	// is not enough: the caller must be able to open this conversation.
	f := newAIFixture(agentGoverned)
	f.seed("entry-1", "ai:agent-1")
	toggle, access := newToggle(f)
	access.allowed = false

	_, err := toggle.SetAutomation(context.Background(), toggleInput(pauseOff()))

	assert.ErrorIs(t, err, ErrAutomationForbidden)
	assert.Empty(t, f.pauser.paused, "nothing is switched for a caller without access")
	assert.Equal(t, "ai:agent-1", f.owner("entry-1"))
	assert.Equal(t, []string{"bob|ws-1|entry-1|whatsapp"}, access.asked)
}

func TestOperatorToggle_WithoutAnAccessCheckRefusesEverything(t *testing.T) {
	f := newAIFixture(agentGoverned)
	toggle := NewOperatorAutomationToggle(f.pauser, f.svc, nil)

	_, err := toggle.SetAutomation(context.Background(), toggleInput(pauseOn()))

	assert.ErrorIs(t, err, ErrAutomationForbidden)
	assert.Empty(t, f.pauser.paused)
}

// --- pausing ---

func TestOperatorPause_WhoeverPausesTakesOver(t *testing.T) {
	// A paused agent or workflow answers nobody, and whoever stopped it is about
	// to: the conversation becomes theirs, and the session ends in their hands.
	for _, holder := range []string{"ai:agent-1", "workflow:wf-1"} {
		t.Run(holder, func(t *testing.T) {
			f := newAIFixture(agentGoverned)
			f.seed("entry-1", holder)
			toggle, _ := newToggle(f)

			res, err := toggle.SetAutomation(context.Background(), toggleInput(pauseOff()))

			require.NoError(t, err)
			assert.Equal(t, []string{"whatsapp:entry-1"}, f.pauser.paused, "the existing switch does the pausing")
			assert.Equal(t, "bob", f.owner("entry-1"))
			assert.Equal(t, "bob", res.Owner)
			assert.Equal(t, []string{"ws-1|entry-1|handed_off|automation_paused|bob"}, f.sessions.ended,
				"a paused AI did not resolve the conversation; bob took it")
		})
	}
}

func TestOperatorPause_SomeoneWhoCannotReceiveLeavesItToTheTeam(t *testing.T) {
	f := newAIFixture(agentGoverned)
	f.seed("entry-1", "ai:agent-1")
	f.receivers.denied["bob"] = true
	toggle, _ := newToggle(f)

	res, err := toggle.SetAutomation(context.Background(), toggleInput(pauseOff()))

	require.NoError(t, err)
	assert.Equal(t, "", f.owner("entry-1"), "the team queue gets it back")
	assert.Equal(t, "", res.Owner)
}

func TestOperatorPause_LeavesAPersonsConversationAlone(t *testing.T) {
	f := newAIFixture(agentGoverned)
	f.seed("entry-1", "bob")
	toggle, _ := newToggle(f)

	res, err := toggle.SetAutomation(context.Background(), toggleInput(pauseOff()))

	require.NoError(t, err)
	assert.Equal(t, "bob", f.owner("entry-1"))
	assert.Equal(t, "bob", res.Owner)
	assert.Equal(t, []string{"ws-1|entry-1|handed_off|automation_paused|bob"}, f.sessions.ended,
		"an AI replying beside a person steps out to that person")
}

func TestOperatorPause_AFailedSwitchReleasesNothing(t *testing.T) {
	f := newAIFixture(agentGoverned)
	f.seed("entry-1", "ai:agent-1")
	f.pauser.err = errors.New("db down")
	toggle, _ := newToggle(f)

	_, err := toggle.SetAutomation(context.Background(), toggleInput(pauseOff()))

	require.Error(t, err)
	assert.Equal(t, "ai:agent-1", f.owner("entry-1"), "an automation that is still on keeps its conversation")
	assert.Empty(t, f.sessions.ended)
}

func TestOperatorPause_AFailedReleaseSurfaces(t *testing.T) {
	// The pause is stored but the conversation may still be hidden; the operator
	// must hear it so they can retry (both steps are idempotent).
	f := newAIFixture(agentGoverned)
	f.seed("entry-1", "ai:agent-1")
	f.svc.workspaceResolver = &mockResolver{workspaceErr: errors.New("gone")}
	toggle, _ := newToggle(f)

	_, err := toggle.SetAutomation(context.Background(), toggleInput(pauseOff()))

	require.Error(t, err)
}

// --- resuming hands the conversation back ---

func TestOperatorResume_HandsAPersonsConversationBackToTheAutomation(t *testing.T) {
	// Replies read only the switch. Resuming while a person keeps the
	// conversation would have the AI and the person answering the same contact.
	cases := map[string]struct {
		profile   func() *aiFixture
		wantOwner string
	}{
		"agent":    {profile: func() *aiFixture { return newAIFixture(agentGoverned) }, wantOwner: "ai:agent-1"},
		"workflow": {profile: func() *aiFixture { return newAIFixture(workflowGoverned) }, wantOwner: "workflow:wf-1"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := tc.profile()
			f.seed("entry-1", "bob")
			toggle, _ := newToggle(f)

			res, err := toggle.SetAutomation(context.Background(), toggleInput(pauseOn()))

			require.NoError(t, err)
			assert.Equal(t, []string{"resume:entry-1"}, f.pauser.paused)
			assert.Equal(t, tc.wantOwner, f.owner("entry-1"))
			assert.Equal(t, tc.wantOwner, res.Owner)
		})
	}
}

func TestOperatorResume_WithNothingToAnswerOnlyFlipsTheSwitch(t *testing.T) {
	// No agent or workflow is configured: handing back would give the contact
	// to nobody, so the person keeps it and the switch still flips.
	f := newAIFixture(noAutomation)
	f.seed("entry-1", "bob")
	toggle, _ := newToggle(f)

	res, err := toggle.SetAutomation(context.Background(), toggleInput(pauseOn()))

	require.NoError(t, err)
	assert.Equal(t, []string{"resume:entry-1"}, f.pauser.paused)
	assert.Equal(t, "bob", f.owner("entry-1"))
	assert.Equal(t, "bob", res.Owner)
}

func TestOperatorResume_AFailedHandBackSurfaces(t *testing.T) {
	f := newAIFixture(agentGoverned)
	f.seed("entry-1", "bob")
	f.profiles.err = errors.New("db down")
	toggle, _ := newToggle(f)

	_, err := toggle.SetAutomation(context.Background(), toggleInput(pauseOn()))

	require.Error(t, err, "the automation is on while a person still holds it: the caller must retry")
	assert.Equal(t, "bob", f.owner("entry-1"))
}

func TestOperatorInherit_AlsoHandsBack(t *testing.T) {
	// Clearing the override means "follow the channel", which is on.
	f := newAIFixture(agentGoverned)
	f.seed("entry-1", "bob")
	toggle, _ := newToggle(f)

	_, err := toggle.SetAutomation(context.Background(), toggleInput(nil))

	require.NoError(t, err)
	assert.Equal(t, "ai:agent-1", f.owner("entry-1"))
}
