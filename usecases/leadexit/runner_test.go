package leadexit_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/actor"
	"vozko/domain/buttonaction"
	"vozko/domain/calls/calllist"
	"vozko/domain/lead"
	"vozko/domain/leadexit"
	"vozko/domain/workflow"
	opportunity_usecase "vozko/usecases/opportunity"
	workflow_usecase "vozko/usecases/workflow"
)

func tapFixture(t *testing.T, action buttonaction.ButtonAction) *fixture {
	t.Helper()
	f := newFixture(t, storedLead("lead-1", leadNumber))
	f.sentPromo("wamid.tmpl", leadNumber)
	f.promoButton("vz1:0", action)
	return f
}

func tap(t *testing.T, f *fixture, replier *fakeReplier) leadexit.Result {
	t.Helper()
	result, err := f.step.Run(context.Background(), tapOn("wamid.in", "wamid.tmpl", "vz1:0", "Botão"), replier)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return result
}

func TestAHandOffToATeamRunsThroughTheRoulette(t *testing.T) {
	f := tapFixture(t, buttonaction.ButtonAction{ButtonIndex: 0, ButtonText: "Falar com atendente", Kind: buttonaction.KindHandOff,
		Params: buttonaction.Params{DepartmentID: "dep-1"}, Confirmation: "Já chamamos alguém."})
	replier := &fakeReplier{}

	result := tap(t, f, replier)

	if result.OptedOut || len(f.handOff.roulette) != 1 {
		t.Fatalf("result %+v, roulette %+v", result, f.handOff.roulette)
	}
	got := f.handOff.roulette[0]
	if got.WorkspaceID != workspaceA || got.EntryID != "entry-1" || got.EntryType != "whatsapp" || got.DepartmentID != "dep-1" || got.ByActorID != actor.SystemID {
		t.Fatalf("roulette = %+v", got)
	}
	if len(replier.sent) != 1 || replier.sent[0] != "Já chamamos alguém." {
		t.Fatalf("sent = %+v", replier.sent)
	}
	if line := f.events.last(t); line["kind"] != "hand_off" || line["department_id"] != "dep-1" || line["assigned_user_id"] != "user-9" || line["outcome"] != "changed" {
		t.Fatalf("line = %+v", line)
	}
}

func TestAHandOffToAMemberRunsThroughTheDirectHandOff(t *testing.T) {
	f := tapFixture(t, buttonaction.ButtonAction{ButtonIndex: 0, ButtonText: "Falar com a Ana", Kind: buttonaction.KindHandOff, Params: buttonaction.Params{UserID: "user-ana"}})

	tap(t, f, &fakeReplier{})

	if len(f.handOff.humans) != 1 || f.handOff.humans[0] != "user-ana" || len(f.handOff.roulette) != 0 {
		t.Fatalf("humans %+v, roulette %+v", f.handOff.humans, f.handOff.roulette)
	}
	if line := f.events.last(t); line["assigned_user_id"] != "user-ana" {
		t.Fatalf("line = %+v", line)
	}
}

func TestAConsentButtonGrantsWithItsPurpose(t *testing.T) {
	f := tapFixture(t, buttonaction.ButtonAction{ButtonIndex: 0, ButtonText: "Quero receber", Kind: buttonaction.KindConsentGrant,
		Params: buttonaction.Params{Purpose: "ofertas da loja pelo WhatsApp"}})

	result := tap(t, f, &fakeReplier{})

	stored := f.leads.get(t, "lead-1")
	if result.OptedOut || stored.WhatsAppOptIn == nil || stored.ConsentRecord == nil || stored.ConsentRecord.Source != lead.ConsentButtonReply ||
		stored.ConsentRecord.Purpose != "ofertas da loja pelo WhatsApp" || stored.ConsentRecord.Evidence.Ref != "wamid.in" {
		t.Fatalf("lead = %+v, record = %+v", stored, stored.ConsentRecord)
	}
}

func TestAClassifyButtonWritesThroughTheProfilePath(t *testing.T) {
	f := tapFixture(t, buttonaction.ButtonAction{ButtonIndex: 0, ButtonText: "Sou cliente", Kind: buttonaction.KindClassify,
		Params: buttonaction.Params{FieldKey: "perfil", Option: "cliente"}})

	tap(t, f, &fakeReplier{})

	if len(f.profiles.updates) != 1 {
		t.Fatalf("updates = %+v", f.profiles.updates)
	}
	got := f.profiles.updates[0]
	if got.WorkspaceID != workspaceA || got.LeadID != "lead-1" || got.Source != lead.ProfileFromConversation || got.Actor != actor.SystemID ||
		got.Profile.CustomFields["perfil"] != "cliente" {
		t.Fatalf("update = %+v", got)
	}
}

func TestADealButtonCreatesThroughTheEntryDealCommand(t *testing.T) {
	f := tapFixture(t, buttonaction.ButtonAction{ButtonIndex: 0, ButtonText: "Quero comprar", Kind: buttonaction.KindCreateDeal,
		Params: buttonaction.Params{PipelineID: "pipe-1", StageID: "stage-1"}})

	tap(t, f, &fakeReplier{})

	if len(f.deals.commands) != 1 {
		t.Fatalf("commands = %+v", f.deals.commands)
	}
	got := f.deals.commands[0]
	if got.EntryID != "entry-1" || got.EntryType != "whatsapp" || got.LeadID != "lead-1" || got.PipelineID != "pipe-1" || got.StageID != "stage-1" ||
		got.Action != opportunity_usecase.EntryCreate || got.Actor != actor.SystemID {
		t.Fatalf("command = %+v", got)
	}
}

func TestAStartWorkflowButtonStartsThatWorkflowOnceWithTheTapAndItsMessage(t *testing.T) {
	f := tapFixture(t, buttonaction.ButtonAction{ButtonIndex: 2, ButtonText: "Ver opções", Kind: buttonaction.KindStartWorkflow,
		Params: buttonaction.Params{WorkflowID: "wf-1"}, Confirmation: "Vamos lá."})
	replier := &fakeReplier{}
	in := tapOn("wamid.in", "wamid.tmpl", "vz1:0", "Ver opções")
	in.TriggerMessageID = "row-in"

	for i := 0; i < 2; i++ {
		if _, err := f.step.Run(context.Background(), in, replier); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}

	if len(f.workflows.starts) != 1 {
		t.Fatalf("starts = %+v, want exactly one", f.workflows.starts)
	}
	got := f.workflows.starts[0]
	if got.WorkflowID != "wf-1" || got.WorkspaceID != workspaceA || got.EntryID != "entry-1" || got.EntryType != "whatsapp" {
		t.Fatalf("start = %+v", got)
	}
	if got.Data[workflow.DataKeyTriggerMessageID] != "row-in" || got.Data[workflow.DataKeySelectedOptionID] != workflow.TemplateButtonOptionID(2) ||
		got.Data[workflow.DataKeySelectedOptionTitle] != "Ver opções" || got.Data[workflow.DataKeySelectedOptionKind] != workflow.OptionKindTemplateButton ||
		got.Data[workflow.DataKeyContactNumber] != leadNumber || got.Data["message"] != "Ver opções" || got.Data[workflow.VarChannel] != "whatsapp" {
		t.Fatalf("data = %+v", got.Data)
	}
	if len(replier.sent) != 1 || replier.sent[0] != "Vamos lá." {
		t.Fatalf("sent = %+v", replier.sent)
	}
	if line := f.events.last(t); line["kind"] != "start_workflow" || line["workflow_id"] != "wf-1" || line["outcome"] != "changed" {
		t.Fatalf("line = %+v", line)
	}
}

func TestAStartWorkflowButtonOnARunningWorkflowChangesNothing(t *testing.T) {
	f := tapFixture(t, buttonaction.ButtonAction{ButtonIndex: 0, ButtonText: "Ver opções", Kind: buttonaction.KindStartWorkflow,
		Params: buttonaction.Params{WorkflowID: "wf-1"}, Confirmation: "Vamos lá."})
	f.workflows.started = workflow_usecase.StartedRun{RunID: "run-1", AlreadyRunning: true}
	replier := &fakeReplier{}

	tap(t, f, replier)

	if run := f.runs.of("wamid.in"); run.Outcome != leadexit.OutcomeUnchanged {
		t.Fatalf("run = %+v", run)
	}
	if len(replier.sent) != 0 {
		t.Fatalf("sent = %+v", replier.sent)
	}
}

func TestAStartWorkflowButtonWhoseWorkflowCannotStartIsRefusedWithItsReason(t *testing.T) {
	f := tapFixture(t, buttonaction.ButtonAction{ButtonIndex: 0, ButtonText: "Ver opções", Kind: buttonaction.KindStartWorkflow,
		Params: buttonaction.Params{WorkflowID: "wf-1"}})
	f.workflows.err = workflow.ErrWorkflowNotActive

	tap(t, f, &fakeReplier{})

	if run := f.runs.of("wamid.in"); run.Outcome != leadexit.OutcomeRefused || run.Reason != "workflow_not_active" {
		t.Fatalf("run = %+v", run)
	}
	if line := f.events.last(t); line["outcome"] != "refused" || line["reason"] != "workflow_not_active" {
		t.Fatalf("line = %+v", line)
	}
}

func TestACallListButtonAddsTheLeadOnceThroughTheListRule(t *testing.T) {
	f := tapFixture(t, buttonaction.ButtonAction{ButtonIndex: 0, ButtonText: "Me liga", Kind: buttonaction.KindCallList,
		Params: buttonaction.Params{CallListID: "list-1"}, Confirmation: "Vamos te ligar."})
	replier := &fakeReplier{}

	tap(t, f, replier)
	tap(t, f, replier)

	if len(f.callLists.adds) != 1 {
		t.Fatalf("adds = %+v, want exactly one", f.callLists.adds)
	}
	if got := f.callLists.adds[0]; got != (calllist.LeadAddition{WorkspaceID: workspaceA, ListID: "list-1", LeadID: "lead-1"}) {
		t.Fatalf("add = %+v", got)
	}
	if len(replier.sent) != 1 || replier.sent[0] != "Vamos te ligar." {
		t.Fatalf("sent = %+v", replier.sent)
	}
	if line := f.events.last(t); line["kind"] != "call_list" || line["call_list_id"] != "list-1" || line["outcome"] != "changed" {
		t.Fatalf("line = %+v", line)
	}
}

func TestACallListButtonForALeadAlreadyOnTheListChangesNothing(t *testing.T) {
	f := tapFixture(t, buttonaction.ButtonAction{ButtonIndex: 0, ButtonText: "Me liga", Kind: buttonaction.KindCallList,
		Params: buttonaction.Params{CallListID: "list-1"}, Confirmation: "Vamos te ligar."})
	f.callLists.alreadyThere = true
	replier := &fakeReplier{}

	tap(t, f, replier)

	if run := f.runs.of("wamid.in"); run.Outcome != leadexit.OutcomeUnchanged || len(replier.sent) != 0 {
		t.Fatalf("run = %+v, sent = %+v", run, replier.sent)
	}
}

func TestACallListButtonForALeadTheListCannotCallIsRefused(t *testing.T) {
	f := tapFixture(t, buttonaction.ButtonAction{ButtonIndex: 0, ButtonText: "Me liga", Kind: buttonaction.KindCallList,
		Params: buttonaction.Params{CallListID: "list-1"}, Confirmation: "Vamos te ligar."})
	f.callLists.err = calllist.AdmissionRefused(calllist.SkipReason(lead.DialRefusedOptedOut))
	replier := &fakeReplier{}

	tap(t, f, replier)

	if run := f.runs.of("wamid.in"); run.Outcome != leadexit.OutcomeRefused || run.Reason != "call_list_lead_not_admitted" {
		t.Fatalf("run = %+v", run)
	}
	if len(replier.sent) != 0 {
		t.Fatalf("sent = %+v", replier.sent)
	}
}

func TestACallListStillBeingBuiltFailsTheTapSoItIsTriedAgain(t *testing.T) {
	f := tapFixture(t, buttonaction.ButtonAction{ButtonIndex: 0, ButtonText: "Me liga", Kind: buttonaction.KindCallList,
		Params: buttonaction.Params{CallListID: "list-1"}})
	f.callLists.err = calllist.ErrListBuilding
	in := tapOn("wamid.in", "wamid.tmpl", "vz1:0", "Me liga")

	if _, err := f.step.Run(context.Background(), in, &fakeReplier{}); err == nil {
		t.Fatal("a list still being built must fail the inbound so it is retried")
	}
	f.callLists.err = nil
	if _, err := f.step.Run(context.Background(), in, &fakeReplier{}); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if len(f.callLists.adds) != 2 || f.runs.of("wamid.in").Outcome != leadexit.OutcomeChanged {
		t.Fatalf("adds = %d, run = %+v", len(f.callLists.adds), f.runs.of("wamid.in"))
	}
}

func TestAFailedActionIsWrittenOnTheConversationAndTriedAgain(t *testing.T) {
	f := tapFixture(t, buttonaction.ButtonAction{ButtonIndex: 0, ButtonText: "Falar com atendente", Kind: buttonaction.KindHandOff,
		Params: buttonaction.Params{DepartmentID: "dep-1"}})
	f.handOff.err = errors.New("database down")
	in := tapOn("wamid.in", "wamid.tmpl", "vz1:0", "Falar com atendente")

	if _, err := f.step.Run(context.Background(), in, &fakeReplier{}); err == nil {
		t.Fatal("a failed action must fail the inbound so it is retried")
	}
	if run := f.runs.of("wamid.in"); run.Outcome != leadexit.OutcomeFailed || run.Reason != "lead_exit_action_failed" {
		t.Fatalf("run = %+v", run)
	}
	if line := f.events.last(t); line["outcome"] != "failed" || line["reason"] != "lead_exit_action_failed" {
		t.Fatalf("line = %+v", line)
	}

	f.handOff.err = nil
	if _, err := f.step.Run(context.Background(), in, &fakeReplier{}); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if len(f.handOff.roulette) != 2 || f.runs.of("wamid.in").Outcome != leadexit.OutcomeChanged {
		t.Fatalf("retry did not run: %d, %+v", len(f.handOff.roulette), f.runs.of("wamid.in"))
	}
}

func TestARefusedHandOffIsNotRetried(t *testing.T) {
	f := tapFixture(t, buttonaction.ButtonAction{ButtonIndex: 0, ButtonText: "Falar com atendente", Kind: buttonaction.KindHandOff,
		Params: buttonaction.Params{UserID: "user-gone"}})
	f.handOff.err = lead.ErrLeadNotFound

	tap(t, f, &fakeReplier{})

	if run := f.runs.of("wamid.in"); run.Outcome != leadexit.OutcomeRefused || run.Reason != "lead_not_found" {
		t.Fatalf("run = %+v", run)
	}
}

func TestAnInfrastructureFailureResolvingTheTapFailsTheInbound(t *testing.T) {
	f := sairFixture(t)
	f.buttons.err = errors.New("redis down")

	if _, err := f.step.Run(context.Background(), tapOn("wamid.in", "wamid.tmpl", "vz1:1", "SAIR"), &fakeReplier{}); err == nil {
		t.Fatal("want the error so the inbound retry runs it again")
	}
}
