package node_executors

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"vozko/domain/conversation"
	lead_domain "vozko/domain/lead"
	"vozko/domain/shared"
	"vozko/domain/workflow"
	lead_usecase "vozko/usecases/lead"
)

type consentCommandsStub struct {
	calls   []lead_domain.ConsentChange
	leads   []string
	outcome lead_usecase.ConsentOutcome
	err     error
}

func (s *consentCommandsStub) Record(_ context.Context, workspaceID, leadID string, change lead_domain.ConsentChange) (lead_usecase.ConsentOutcome, error) {
	s.calls = append(s.calls, change)
	s.leads = append(s.leads, workspaceID+"/"+leadID)
	if s.err != nil {
		return lead_usecase.ConsentOutcome{}, s.err
	}
	return s.outcome, nil
}

type runMessages map[string]*conversation.Message

func (m runMessages) GetByID(id string) (*conversation.Message, error) {
	if found, ok := m[id]; ok {
		return found, nil
	}
	return nil, conversation.ErrMessageNotFound
}

func leadMessagesOfTheRun() runMessages {
	at := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	from := func(id, entry, text string) *conversation.Message {
		return &conversation.Message{ID: id, EntryID: entry, EntryType: shared.EntryType("unofficial_whatsapp"), Text: text, SentBy: conversation.SentByContact("5511"), CreatedAt: at}
	}
	return runMessages{
		"msg-1":     from("msg-1", "e-1", "SAIR"),
		"msg-other": from("msg-other", "e-9", "SAIR"),
	}
}

func consentNodeDeps(commands *consentCommandsStub) ConsentNodeDeps {
	return ConsentNodeDeps{
		Consent:    commands,
		Messages:   leadMessagesOfTheRun(),
		EntryLeads: &entryLeads{leadID: "lead-1"},
		Leads:      &knownLeadRepo{},
	}
}

func consentEdges() []workflow.Edge {
	return []workflow.Edge{{Source: "n1", Target: "ok", Label: "sucesso"}}
}

func runConsentNode(t *testing.T, deps ConsentNodeDeps, config map[string]interface{}, state map[string]interface{}) (*workflow.NodeResult, error) {
	t.Helper()
	ctx := stageNodeCtx(workflow.NodeTypeActionConsent, config, consentEdges())
	for k, v := range state {
		ctx.State.Set(k, v)
	}
	return NewConsentExecutor(deps).Execute(ctx)
}

func TestWithdrawingConsentCitesTheTriggeringMessage(t *testing.T) {
	commands := &consentCommandsStub{outcome: lead_usecase.ConsentOutcome{Changed: true, EventID: "ev-1"}}
	result, err := runConsentNode(t, consentNodeDeps(commands),
		map[string]interface{}{workflow.ConsentModeKey: workflow.ConsentModeWithdraw},
		map[string]interface{}{workflow.DataKeyTriggerMessageID: "msg-1"})

	require.NoError(t, err)
	require.Empty(t, result.Error)
	require.Equal(t, "ok", result.NextNodeID)
	require.Equal(t, []string{"ws1/lead-1"}, commands.leads)
	require.Equal(t, lead_domain.ConsentChange{
		Kind: lead_domain.ConsentWithdraw, Source: lead_domain.ConsentWorkflow,
		Actor:       lead_domain.ConsentActor{Type: lead_domain.ConsentActorWorkflow, ID: "wf-1"},
		Evidence:    lead_domain.ConsentEvidence{Type: lead_domain.EvidenceMessage, Ref: "msg-1", Excerpt: "SAIR"},
		RequestedBy: lead_domain.OptOutLeadRequest,
	}, commands.calls[0])
	require.Equal(t, true, result.Output["changed"])
	require.Equal(t, "ev-1", result.Output["event_id"])
	require.Equal(t, "lead-1", result.Output["lead_id"])
}

func TestWithdrawingConsentWithoutAMessageCitesTheRunAsANote(t *testing.T) {
	commands := &consentCommandsStub{outcome: lead_usecase.ConsentOutcome{Changed: true}}
	result, err := runConsentNode(t, consentNodeDeps(commands),
		map[string]interface{}{workflow.ConsentModeKey: workflow.ConsentModeWithdraw, workflow.ConsentNoteKey: "lista de {{var.origem}}"},
		map[string]interface{}{"origem": "devolvidos"})

	require.NoError(t, err)
	require.Empty(t, result.Error)
	require.Equal(t, lead_domain.ConsentChange{
		Kind: lead_domain.ConsentWithdraw, Source: lead_domain.ConsentWorkflow,
		Actor:    lead_domain.ConsentActor{Type: lead_domain.ConsentActorWorkflow, ID: "wf-1"},
		Evidence: lead_domain.ConsentEvidence{Type: lead_domain.EvidenceNote, Ref: "workflow_run:run1", Excerpt: "lista de devolvidos"},
	}, commands.calls[0])
}

func TestGrantingConsentCitesTheTemplateButtonTap(t *testing.T) {
	commands := &consentCommandsStub{outcome: lead_usecase.ConsentOutcome{Changed: true, EventID: "ev-2"}}
	state := map[string]interface{}{workflow.DataKeyTriggerMessageID: "msg-1"}
	workflow.ApplySelection(state, workflow.TemplateButtonSelection("Quero receber", 0, true))
	result, err := runConsentNode(t, consentNodeDeps(commands),
		map[string]interface{}{workflow.ConsentModeKey: workflow.ConsentModeGrant, workflow.ConsentPurposeKey: "ofertas"}, state)

	require.NoError(t, err)
	require.Empty(t, result.Error)
	require.Equal(t, lead_domain.ConsentChange{
		Kind: lead_domain.ConsentGrant, Source: lead_domain.ConsentWorkflow, Purpose: "ofertas",
		Actor:    lead_domain.ConsentActor{Type: lead_domain.ConsentActorWorkflow, ID: "wf-1"},
		Evidence: lead_domain.ConsentEvidence{Type: lead_domain.EvidenceMessage, Ref: "msg-1", Excerpt: "SAIR"},
	}, commands.calls[0])
}

func TestGrantingConsentAfterATypedReplyCitesTheMessage(t *testing.T) {
	commands := &consentCommandsStub{outcome: lead_usecase.ConsentOutcome{Changed: true}}
	_, err := runConsentNode(t, consentNodeDeps(commands),
		map[string]interface{}{workflow.ConsentModeKey: workflow.ConsentModeGrant},
		map[string]interface{}{workflow.DataKeyTriggerMessageID: "msg-1"})
	require.NoError(t, err)
	require.Equal(t, lead_domain.EvidenceMessage, commands.calls[0].Evidence.Type)
	require.Empty(t, commands.calls[0].RequestedBy)
}

func TestTheConsentNodeFailsTheStepInsteadOfContinuing(t *testing.T) {
	cases := []struct {
		name   string
		deps   func(*consentCommandsStub) ConsentNodeDeps
		config map[string]interface{}
		state  map[string]interface{}
		says   string
	}{
		{"a grant without the lead's message", consentNodeDeps,
			map[string]interface{}{workflow.ConsentModeKey: workflow.ConsentModeGrant}, nil, "lead_consent_evidence_required"},
		{"a grant citing a message of another conversation", consentNodeDeps,
			map[string]interface{}{workflow.ConsentModeKey: workflow.ConsentModeGrant},
			map[string]interface{}{workflow.DataKeyTriggerMessageID: "msg-other"}, "lead_consent_evidence_invalid"},
		{"a withdrawal citing an unknown message", consentNodeDeps,
			map[string]interface{}{workflow.ConsentModeKey: workflow.ConsentModeWithdraw},
			map[string]interface{}{workflow.DataKeyTriggerMessageID: "msg-invented"}, "lead_consent_evidence_invalid"},
		{"an unknown mode", consentNodeDeps, map[string]interface{}{workflow.ConsentModeKey: "talvez"}, nil, "registrar"},
		{"a conversation without a lead", func(c *consentCommandsStub) ConsentNodeDeps {
			deps := consentNodeDeps(c)
			deps.EntryLeads = &entryLeads{}
			return deps
		}, map[string]interface{}{workflow.ConsentModeKey: workflow.ConsentModeWithdraw}, nil, "lead_not_found"},
		{"no consent commands here", func(c *consentCommandsStub) ConsentNodeDeps {
			deps := consentNodeDeps(c)
			deps.Consent = nil
			return deps
		}, map[string]interface{}{workflow.ConsentModeKey: workflow.ConsentModeWithdraw}, nil, "indisponível"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			commands := &consentCommandsStub{}
			result, err := runConsentNode(t, tc.deps(commands), tc.config, tc.state)
			require.NoError(t, err)
			require.Contains(t, result.Error, tc.says)
			require.Empty(t, commands.calls)
		})
	}
}

func TestTheConsentNodeFailsWithTheRefusalCode(t *testing.T) {
	commands := &consentCommandsStub{err: lead_domain.ErrConsentActorInvalid}
	result, err := runConsentNode(t, consentNodeDeps(commands),
		map[string]interface{}{workflow.ConsentModeKey: workflow.ConsentModeWithdraw}, nil)
	require.NoError(t, err)
	require.Contains(t, result.Error, "lead_consent_actor_invalid")
}

func TestTheConsentNodeRetriesWhenTheStoreFails(t *testing.T) {
	commands := &consentCommandsStub{err: errors.New("database down")}
	_, err := runConsentNode(t, consentNodeDeps(commands),
		map[string]interface{}{workflow.ConsentModeKey: workflow.ConsentModeWithdraw}, nil)
	require.Error(t, err)

	deps := consentNodeDeps(&consentCommandsStub{})
	deps.EntryLeads = &entryLeads{err: errors.New("database down")}
	_, err = runConsentNode(t, deps, map[string]interface{}{workflow.ConsentModeKey: workflow.ConsentModeWithdraw}, nil)
	require.Error(t, err)
}

func consentConditionEdges() []workflow.Edge {
	return []workflow.Edge{
		{Source: "n1", Target: "yes", Label: workflow.ConsentBranchGranted},
		{Source: "n1", Target: "left", Label: workflow.ConsentBranchWithdrawn},
		{Source: "n1", Target: "unknown", Label: workflow.ConsentBranchNone},
	}
}

func checkConsent(deps ConsentNodeDeps) (*workflow.NodeResult, error) {
	ctx := stageNodeCtx(workflow.NodeTypeConditionConsent, map[string]interface{}{}, consentConditionEdges())
	return NewCheckConsentExecutor(deps).Execute(ctx)
}

func TestTheConsentConditionFollowsTheLeadsState(t *testing.T) {
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name  string
		lead  *lead_domain.Lead
		entry *entryLeads
		want  string
	}{
		{"granted", &lead_domain.Lead{ID: "lead-1", WhatsAppOptIn: &lead_domain.Consent{GrantedAt: at, Source: lead_domain.ConsentManual}}, &entryLeads{leadID: "lead-1"}, "yes"},
		{"withdrawn", optedOutLead(), &entryLeads{leadID: "lead-1"}, "left"},
		{"nothing recorded", &lead_domain.Lead{ID: "lead-1"}, &entryLeads{leadID: "lead-1"}, "unknown"},
		{"a conversation without a lead", nil, &entryLeads{}, "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deps := consentNodeDeps(&consentCommandsStub{})
			deps.EntryLeads, deps.Leads = tc.entry, &knownLeadRepo{byID: tc.lead}
			result, err := checkConsent(deps)
			require.NoError(t, err)
			require.Empty(t, result.Error)
			require.Equal(t, tc.want, result.NextNodeID)
		})
	}
}

func TestTheConsentConditionNeverGuessesABranch(t *testing.T) {
	deps := consentNodeDeps(&consentCommandsStub{})
	deps.Leads = &knownLeadRepo{err: errors.New("database down")}
	_, err := checkConsent(deps)
	require.Error(t, err)

	deps = consentNodeDeps(&consentCommandsStub{})
	deps.Leads = nil
	result, err := checkConsent(deps)
	require.NoError(t, err)
	require.Contains(t, result.Error, "indisponível")

	deps = consentNodeDeps(&consentCommandsStub{})
	deps.Leads = &knownLeadRepo{byID: &lead_domain.Lead{ID: "lead-1"}}
	ctx := stageNodeCtx(workflow.NodeTypeConditionConsent, map[string]interface{}{}, consentConditionEdges()[:2])
	result, err = NewCheckConsentExecutor(deps).Execute(ctx)
	require.NoError(t, err)
	require.Contains(t, result.Error, workflow.ConsentBranchNone)
}

func TestAFlowRoutesByTheTappedTemplateButton(t *testing.T) {
	route := func(tapped int, config map[string]interface{}) string {
		data := map[string]interface{}{}
		workflow.ApplySelection(data, workflow.TemplateButtonSelection("SAIR", tapped, true))
		ctx := stageNodeCtx(workflow.NodeTypeConditionBranch, config, checkEdges())
		for k, v := range data {
			ctx.State.Set(k, v)
		}
		result, err := NewConditionBranchExecutor().Execute(ctx)
		require.NoError(t, err)
		return result.NextNodeID
	}
	byIndex := map[string]interface{}{"variable": "{{selected_option_id}}", "operator": "eq", "value": workflow.TemplateButtonOptionID(1)}
	require.Equal(t, "yes", route(1, byIndex))
	require.Equal(t, "no", route(0, byIndex))

	byText := map[string]interface{}{"variable": "{{selected_option_title}}", "operator": "eq_ci", "value": "sair"}
	require.Equal(t, "yes", route(2, byText))
}

func TestConsentNodesAreInTheCatalog(t *testing.T) {
	action := NewConsentExecutor(ConsentNodeDeps{}).Definition()
	condition := NewCheckConsentExecutor(ConsentNodeDeps{}).Definition()
	require.Equal(t, workflow.NodeTypeActionConsent, action.Type)
	require.Equal(t, workflow.NodeTypeConditionConsent, condition.Type)
	require.NotEmpty(t, action.Guidance.When)
	require.NotEmpty(t, condition.Guidance.When)
	outputs := map[string]bool{}
	for _, h := range condition.Outputs {
		outputs[h.ID] = !h.Optional
	}
	require.Equal(t, map[string]bool{workflow.ConsentBranchGranted: true, workflow.ConsentBranchWithdrawn: true, workflow.ConsentBranchNone: true}, outputs)
	var mode workflow.ConfigField
	for _, f := range action.ConfigSchema {
		if f.Key == workflow.ConsentModeKey {
			mode = f
		}
	}
	require.True(t, mode.Required)
	require.Len(t, mode.Options, 2)
	require.Empty(t, action.DefaultConfig[workflow.ConsentModeKey], "nothing is chosen for the user")
}
