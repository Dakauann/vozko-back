package inbox_assignment_usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"vozko/domain/agent"
	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

type profileBackedDelegations struct {
	profiles *stubAutomationProfiles
	saved    []conversation.Automation
	deleted  int
	err      error
}

func (d *profileBackedDelegations) Find(context.Context, string, shared.EntryType) (*conversation.Delegation, error) {
	if d.profiles.profile.Delegate == nil {
		return nil, d.err
	}
	return &conversation.Delegation{Automation: *d.profiles.profile.Delegate}, d.err
}

func (d *profileBackedDelegations) FindMany(context.Context, []shared.EntryRef) (map[shared.EntryRef]conversation.Automation, error) {
	return nil, nil
}

func (d *profileBackedDelegations) Save(_ context.Context, del conversation.Delegation) error {
	if d.err != nil {
		return d.err
	}
	a := del.Automation
	d.profiles.profile.Delegate = &a
	d.saved = append(d.saved, a)
	return nil
}

func (d *profileBackedDelegations) Delete(context.Context, string, shared.EntryType) error {
	d.profiles.profile.Delegate = nil
	d.deleted++
	return d.err
}

type agentDirectory map[string]*agent.Agent

func (a agentDirectory) FindByID(id string) (*agent.Agent, error) {
	if found, ok := a[id]; ok {
		return found, nil
	}
	return nil, errors.New("not found")
}

type workflowDirectory map[string]*workflow.Workflow

func (w workflowDirectory) FindByID(id string) (*workflow.Workflow, error) {
	if found, ok := w[id]; ok {
		return found, nil
	}
	return nil, errors.New("not found")
}

func newDelegation(f *aiFixture) (*AutomationDelegation, *profileBackedDelegations, *stubAccess) {
	store := &profileBackedDelegations{profiles: f.profiles}
	toggle, access := newToggle(f)
	return NewAutomationDelegation(store, toggle, AutomationDirectory{
		Agents: agentDirectory{
			"agent-9":   {ID: "agent-9", WorkspaceID: "ws-1", IsActive: true},
			"off":       {ID: "off", WorkspaceID: "ws-1", IsActive: false},
			"elsewhere": {ID: "elsewhere", WorkspaceID: "ws-2", IsActive: true},
		},
		Workflows: workflowDirectory{
			"wf-9":  {ID: "wf-9", WorkspaceID: "ws-1", Status: workflow.WorkflowStatusActive},
			"draft": {ID: "draft", WorkspaceID: "ws-1", Status: workflow.WorkflowStatusDraft},
			"ivr":   {ID: "ivr", WorkspaceID: "ws-1", Status: workflow.WorkflowStatusActive, Type: workflow.WorkflowTypeVoice},
		},
	}), store, access
}

func delegateInput(kind conversation.AutomationKind, id string) DelegateInput {
	return DelegateInput{
		ActorUserID: "bob", WorkspaceID: "ws-1", EntryID: "entry-1", EntryType: shared.EntryTypeWhatsApp,
		Automation: conversation.Automation{Kind: kind, ID: id},
	}
}

func TestDelegateHandsAConversationToTheChosenAutomationOnAChannelWithoutOne(t *testing.T) {
	for kind, id := range map[conversation.AutomationKind]string{
		conversation.AutomationAgent:    "agent-9",
		conversation.AutomationWorkflow: "wf-9",
	} {
		t.Run(string(kind), func(t *testing.T) {
			f := newAIFixture(noAutomation)
			f.seed("entry-1", "bob")
			delegation, store, _ := newDelegation(f)

			res, err := delegation.Delegate(context.Background(), delegateInput(kind, id))

			require.NoError(t, err)
			want := conversation.Automation{Kind: kind, ID: id}.ActorID()
			assert.Equal(t, want, f.owner("entry-1"))
			assert.Equal(t, want, res.Owner)
			assert.Equal(t, []string{"resume:entry-1"}, f.pauser.paused, "the automation is switched on")
			assert.Len(t, store.saved, 1)
		})
	}
}

func TestDelegateRefusesAutomationsThatCannotAnswer(t *testing.T) {
	cases := map[string]DelegateInput{
		"agent from another workspace": delegateInput(conversation.AutomationAgent, "elsewhere"),
		"inactive agent":               delegateInput(conversation.AutomationAgent, "off"),
		"unknown agent":                delegateInput(conversation.AutomationAgent, "ghost"),
		"draft workflow":               delegateInput(conversation.AutomationWorkflow, "draft"),
		"voice workflow":               delegateInput(conversation.AutomationWorkflow, "ivr"),
		"not an automation":            delegateInput("person", "bob"),
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			f := newAIFixture(noAutomation)
			f.seed("entry-1", "bob")
			delegation, store, _ := newDelegation(f)

			_, err := delegation.Delegate(context.Background(), in)

			require.Error(t, err)
			assert.Empty(t, store.saved, "nothing is stored")
			assert.Empty(t, f.pauser.paused, "nothing is switched")
			assert.Equal(t, "bob", f.owner("entry-1"))
		})
	}
}

func TestDelegateRefusesACallerWithoutAccessBeforeStoringAnything(t *testing.T) {
	f := newAIFixture(noAutomation)
	f.seed("entry-1", "bob")
	delegation, store, access := newDelegation(f)
	access.allowed = false

	_, err := delegation.Delegate(context.Background(), delegateInput(conversation.AutomationAgent, "agent-9"))

	assert.ErrorIs(t, err, ErrAutomationForbidden)
	assert.Empty(t, store.saved)
	assert.Equal(t, "bob", f.owner("entry-1"))
}

func TestAFailedHandOverRestoresThePreviousDelegation(t *testing.T) {
	f := newAIFixture(noAutomation)
	f.seed("entry-1", "bob")
	previous := conversation.Automation{Kind: conversation.AutomationWorkflow, ID: "wf-old"}
	f.profiles.profile.Delegate = &previous
	f.profiles.err = errors.New("db down")
	delegation, _, _ := newDelegation(f)

	_, err := delegation.Delegate(context.Background(), delegateInput(conversation.AutomationAgent, "agent-9"))

	require.Error(t, err)
	assert.Equal(t, previous, *f.profiles.profile.Delegate)
	assert.Equal(t, "bob", f.owner("entry-1"))
	assert.Equal(t, []string{"resume:entry-1", "whatsapp:entry-1"}, f.pauser.paused, "the automation stays paused")
}

func TestTheDelegationSurvivesAPauseAndComesBackOnResume(t *testing.T) {
	f := newAIFixture(agentGoverned)
	f.seed("entry-1", "bob")
	delegation, _, _ := newDelegation(f)
	toggle, _ := newToggle(f)

	_, err := delegation.Delegate(context.Background(), delegateInput(conversation.AutomationWorkflow, "wf-9"))
	require.NoError(t, err)

	f.profiles.profile.AutomationEnabled = pauseOff()
	_, err = toggle.SetAutomation(context.Background(), toggleInput(pauseOff()))
	require.NoError(t, err)
	assert.Equal(t, "bob", f.owner("entry-1"))

	f.profiles.profile.AutomationEnabled = pauseOn()
	res, err := toggle.SetAutomation(context.Background(), toggleInput(pauseOn()))
	require.NoError(t, err)
	assert.Equal(t, "workflow:wf-9", res.Owner, "resuming returns to the delegated workflow, not the channel's agent")
}
