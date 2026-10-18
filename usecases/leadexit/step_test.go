package leadexit_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/buttonaction"
	"vozko/domain/campaign"
	ce "vozko/domain/conversation_event"
	"vozko/domain/lead"
	"vozko/domain/leadexit"
	wo "vozko/domain/whatsapp_outreach"
	"vozko/domain/workflow"
	buttonaction_usecase "vozko/usecases/buttonaction"
)

const sairConfirmation = "Entendido. Você não receberá mais mensagens."

func sairFixture(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t, storedLead("lead-1", leadNumber))
	f.sentPromo("wamid.tmpl", leadNumber)
	f.promoButton("vz1:1", buttonaction.ButtonAction{ButtonIndex: 1, ButtonText: "SAIR", Kind: buttonaction.KindOptOut, Confirmation: sairConfirmation})
	return f
}

func TestATappedSairOnAnOfficialTemplateRecordsAWithdrawalWithTheTapAsEvidenceAndOneConfirmation(t *testing.T) {
	f := sairFixture(t)
	replier := &fakeReplier{}

	result, err := f.step.Run(context.Background(), tapOn("wamid.in", "wamid.tmpl", "vz1:1", "SAIR"), replier)

	if err != nil || !result.OptedOut {
		t.Fatalf("Run = %+v, %v", result, err)
	}
	stored := f.leads.get(t, "lead-1")
	if stored.OptedOutAt == nil || stored.OptOutSource != lead.OptOutLeadRequest {
		t.Fatalf("lead not opted out: %+v", stored)
	}
	record := stored.ConsentRecord
	if record == nil || record.Kind != lead.ConsentWithdraw || record.Source != lead.ConsentButtonReply ||
		record.Actor.Type != lead.ConsentActorLead || record.Actor.ID != "lead-1" ||
		record.Evidence.Type != lead.EvidenceButtonReply || record.Evidence.Ref != "wamid.in" || record.Evidence.Excerpt != "SAIR" {
		t.Fatalf("record = %+v", record)
	}
	if record.ConfirmationSentAt == nil {
		t.Fatal("the confirmation was not claimed")
	}
	if len(replier.sent) != 1 || replier.sent[0] != sairConfirmation {
		t.Fatalf("sent = %+v", replier.sent)
	}
	if call := f.buttons.tapCalls[0]; call.WorkspaceID != workspaceA || call.TemplateID != "tmpl-1" || call.CampaignID != "camp-1" || call.Payload != "vz1:1" || call.Text != "SAIR" {
		t.Fatalf("tap = %+v", call)
	}
	if result.Selection == nil || result.Selection.ID != "template_button:1" || result.Selection.Kind != workflow.OptionKindTemplateButton {
		t.Fatalf("selection = %+v", result.Selection)
	}
	line := f.events.last(t)
	if line["kind"] != "opt_out" || line["outcome"] != "changed" || line["trigger"] != "button" || line["button_text"] != "SAIR" ||
		line["confirmation"] != "sent" || line["template_message_id"] != "wamid.tmpl" {
		t.Fatalf("line = %+v", line)
	}
	if f.events.events[0].EventType != ce.EventLeadExitAction || f.events.events[0].EntryID != "entry-1" {
		t.Fatalf("event = %+v", f.events.events[0])
	}
	if run := f.runs.of("wamid.in"); run.Outcome != leadexit.OutcomeChanged || run.Kind != buttonaction.KindOptOut || run.TemplateMessageID != "wamid.tmpl" {
		t.Fatalf("run = %+v", run)
	}
}

func TestTheCustomerFlowsLaterSendIsRefusedWhileTheOneConfirmationWentOut(t *testing.T) {
	f := sairFixture(t)
	replier := &fakeReplier{}
	if _, err := f.step.Run(context.Background(), tapOn("wamid.in", "wamid.tmpl", "vz1:1", "SAIR"), replier); err != nil {
		t.Fatal(err)
	}
	if len(replier.sent) != 1 {
		t.Fatalf("confirmations = %d", len(replier.sent))
	}
	stored := f.leads.get(t, "lead-1")
	if err := campaign.RecipientRefusal(stored); !errors.Is(err, wo.ErrLeadOptedOut) || wo.ErrorCode(err) != "lead_opted_out" {
		t.Fatalf("the flow's send would pass: %v", err)
	}
	if err := stored.ClaimOptOutConfirmation(stored.ConsentRecord.EventID, stepNow); !errors.Is(err, lead.ErrLeadOptedOut) {
		t.Fatalf("a second confirmation would pass: %v", err)
	}
}

func TestASecondTapRecordsNothingAndSendsNothing(t *testing.T) {
	f := sairFixture(t)
	replier := &fakeReplier{}
	if _, err := f.step.Run(context.Background(), tapOn("wamid.in1", "wamid.tmpl", "vz1:1", "SAIR"), replier); err != nil {
		t.Fatal(err)
	}
	savesAfterFirst, eventsAfterFirst := f.leads.saves, len(f.leads.events)

	result, err := f.step.Run(context.Background(), tapOn("wamid.in2", "wamid.tmpl", "vz1:1", "SAIR"), replier)

	if err != nil || !result.OptedOut {
		t.Fatalf("second Run = %+v, %v", result, err)
	}
	if f.leads.saves != savesAfterFirst || len(f.leads.events) != eventsAfterFirst {
		t.Fatalf("the second tap wrote: saves %d -> %d, events %d -> %d", savesAfterFirst, f.leads.saves, eventsAfterFirst, len(f.leads.events))
	}
	if len(replier.sent) != 1 {
		t.Fatalf("confirmations = %+v", replier.sent)
	}
	if run := f.runs.of("wamid.in2"); run.Outcome != leadexit.OutcomeUnchanged || run.Confirmation != leadexit.ConfirmationNone {
		t.Fatalf("run = %+v", run)
	}
}

func TestTheSameInboundMessageRunsItsActionOnce(t *testing.T) {
	f := newFixture(t, storedLead("lead-1", leadNumber))
	f.sentPromo("wamid.tmpl", leadNumber)
	f.promoButton("vz1:0", buttonaction.ButtonAction{ButtonIndex: 0, ButtonText: "Falar com atendente", Kind: buttonaction.KindHandOff,
		Params: buttonaction.Params{DepartmentID: "dep-1"}, Confirmation: "Já chamamos alguém."})
	replier := &fakeReplier{}
	in := tapOn("wamid.in", "wamid.tmpl", "vz1:0", "Falar com atendente")

	for i := 0; i < 2; i++ {
		if _, err := f.step.Run(context.Background(), in, replier); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}

	if len(f.handOff.roulette) != 1 || len(replier.sent) != 1 || len(f.events.events) != 1 {
		t.Fatalf("hand-offs %d, confirmations %d, lines %d", len(f.handOff.roulette), len(replier.sent), len(f.events.events))
	}
}

func TestATapOnAMessageSentToAnotherNumberRunsNothing(t *testing.T) {
	f := sairFixture(t)
	f.sentPromo("wamid.tmpl", otherPhone)
	replier := &fakeReplier{}

	result, err := f.step.Run(context.Background(), tapOn("wamid.in", "wamid.tmpl", "vz1:1", "SAIR"), replier)

	if err != nil || result.OptedOut {
		t.Fatalf("Run = %+v, %v", result, err)
	}
	if len(f.buttons.tapCalls) != 0 || f.leads.saves != 0 || len(replier.sent) != 0 || len(f.events.events) != 0 {
		t.Fatalf("something ran: taps %d, saves %d, sent %d, lines %d", len(f.buttons.tapCalls), f.leads.saves, len(replier.sent), len(f.events.events))
	}
	if result.Selection == nil || result.Selection.ID != "template_button" || result.Selection.Title != "SAIR" {
		t.Fatalf("selection = %+v", result.Selection)
	}
}

func TestATapOnAMessageOfAnotherWorkspaceRunsNothing(t *testing.T) {
	f := sairFixture(t)
	f.sent["wamid.tmpl"].WorkspaceID = "22222222-2222-2222-2222-222222222222"

	result, err := f.step.Run(context.Background(), tapOn("wamid.in", "wamid.tmpl", "vz1:1", "SAIR"), &fakeReplier{})

	if err != nil || result.OptedOut || f.leads.saves != 0 || len(f.buttons.tapCalls) != 0 {
		t.Fatalf("Run = %+v, %v, saves %d", result, err, f.leads.saves)
	}
}

func TestATemplateWordCountsOnlyForLeadsWhoReceivedThatTemplate(t *testing.T) {
	f := newFixture(t, storedLead("lead-1", leadNumber))
	f.buttons.exits["promo_outubro:pt_BR"] = &buttonaction_usecase.TemplateExit{Template: promoRef, Words: []string{"/sair"}, Confirmation: "Saída do modelo."}
	replier := &fakeReplier{}

	stranger, err := f.step.Run(context.Background(), typed("wamid.in1", "/sair"), replier)
	if err != nil || stranger.OptedOut || f.leads.saves != 0 {
		t.Fatalf("without the template: %+v, %v, saves %d", stranger, err, f.leads.saves)
	}

	received := typed("wamid.in2", "/SAIR")
	received.LastTemplate = &leadexit.SentTemplate{Template: promoRef}
	result, err := f.step.Run(context.Background(), received, replier)

	if err != nil || !result.OptedOut {
		t.Fatalf("with the template: %+v, %v", result, err)
	}
	record := f.leads.get(t, "lead-1").ConsentRecord
	if record == nil || record.Source != lead.ConsentKeyword || record.Evidence.Type != lead.EvidenceMessage || record.Evidence.Ref != "wamid.in2" || record.Evidence.Excerpt != "/SAIR" {
		t.Fatalf("record = %+v", record)
	}
	if len(replier.sent) != 1 || replier.sent[0] != "Saída do modelo." {
		t.Fatalf("sent = %+v", replier.sent)
	}
	if line := f.events.last(t); line["trigger"] != "template_word" || line["word"] != "/sair" || line["template_name"] != "promo_outubro" {
		t.Fatalf("line = %+v", line)
	}
}

func TestTheRepliedTemplatesWordCountsToo(t *testing.T) {
	f := newFixture(t, storedLead("lead-1", leadNumber))
	f.sentPromo("wamid.tmpl", leadNumber)
	f.buttons.exits["tmpl-1"] = &buttonaction_usecase.TemplateExit{Template: promoRef, Words: []string{"/sair"}}
	in := typed("wamid.in", "/sair")
	in.ContextMessageID = "wamid.tmpl"

	result, err := f.step.Run(context.Background(), in, &fakeReplier{})

	if err != nil || !result.OptedOut || result.Selection != nil {
		t.Fatalf("Run = %+v, %v", result, err)
	}
}

func TestAShownWordMatchesItsFoldedSlashCommand(t *testing.T) {
	for i, text := range []string{"/Sair", " /sair ", "/SAIR", "/SAÍR"} {
		f := newFixture(t, storedLead("lead-1", leadNumber))
		f.promoShows("sair", sairConfirmation)
		replier := &fakeReplier{}

		result, err := f.step.Run(context.Background(), afterPromo(typed("wamid.in", text)), replier)

		if err != nil || !result.OptedOut {
			t.Fatalf("%d %q: %+v, %v", i, text, result, err)
		}
		record := f.leads.get(t, "lead-1").ConsentRecord
		if record == nil || record.Source != lead.ConsentKeyword || record.Evidence.Excerpt != strings.TrimSpace(text) {
			t.Fatalf("%q: record = %+v", text, record)
		}
		if len(replier.sent) != 1 || replier.sent[0] != sairConfirmation {
			t.Fatalf("%q: sent = %+v", text, replier.sent)
		}
	}
}

func TestTheWordWithoutItsSlashIsNotAnExit(t *testing.T) {
	f := newFixture(t, storedLead("lead-1", leadNumber))
	f.promoShows("sair", sairConfirmation)
	replier := &fakeReplier{}

	for i, text := range []string{"sair", "Sair", " SAIR ", "sáir"} {
		result, err := f.step.Run(context.Background(), afterPromo(typed("wamid."+string(rune('a'+i)), text)), replier)
		if err != nil || result.OptedOut {
			t.Fatalf("%q: %+v, %v", text, result, err)
		}
	}
	if f.leads.saves != 0 || len(replier.sent) != 0 || len(f.events.events) != 0 {
		t.Fatalf("saves %d, sent %d, lines %d", f.leads.saves, len(replier.sent), len(f.events.events))
	}
}

func TestWithoutAReceivedTemplateNoWordIsAnExit(t *testing.T) {
	f := newFixture(t, storedLead("lead-1", leadNumber))
	replier := &fakeReplier{}

	for i, text := range []string{"sair", "SAIR", "/sair", "parar", "stop"} {
		result, err := f.step.Run(context.Background(), typed("wamid."+string(rune('a'+i)), text), replier)
		if err != nil || result.OptedOut {
			t.Fatalf("%q: %+v, %v", text, result, err)
		}
	}
	if f.leads.saves != 0 || len(replier.sent) != 0 || len(f.events.events) != 0 {
		t.Fatalf("saves %d, sent %d, lines %d", f.leads.saves, len(replier.sent), len(f.events.events))
	}
}

func TestASentenceWithTheWordIsNotAnExit(t *testing.T) {
	f := newFixture(t, storedLead("lead-1", leadNumber))
	f.promoShows("sair", "")
	in := typed("wamid.in", "quero sair do plano")
	in.LastTemplate = &leadexit.SentTemplate{Template: promoRef}

	result, err := f.step.Run(context.Background(), in, &fakeReplier{})

	if err != nil || result.OptedOut || f.leads.saves != 0 || len(f.events.events) != 0 {
		t.Fatalf("Run = %+v, %v", result, err)
	}
}

func TestAMessageLongerThanAnyWordReadsNoExitSettings(t *testing.T) {
	f := newFixture(t, storedLead("lead-1", leadNumber))
	f.promoShows("sair", "")
	in := typed("wamid.in", "bom dia, quero saber mais sobre o plano anual")
	in.LastTemplate = &leadexit.SentTemplate{Template: promoRef}

	result, err := f.step.Run(context.Background(), in, &fakeReplier{})

	if err != nil || result.OptedOut {
		t.Fatalf("Run = %+v, %v", result, err)
	}
	if f.buttons.exitCalls != 0 {
		t.Fatalf("a long message read the template exits %d times", f.buttons.exitCalls)
	}
}

func TestNoConfirmationIsSentWhenNoneIsConfigured(t *testing.T) {
	f := newFixture(t, storedLead("lead-1", leadNumber))
	f.promoShows("sair", "")
	replier := &fakeReplier{}

	result, err := f.step.Run(context.Background(), afterPromo(typed("wamid.in", "/sair")), replier)

	if err != nil || !result.OptedOut || len(replier.sent) != 0 {
		t.Fatalf("Run = %+v, %v, sent %+v", result, err, replier.sent)
	}
	if f.leads.get(t, "lead-1").ConsentRecord.ConfirmationSentAt != nil {
		t.Fatal("a confirmation was claimed without being configured")
	}
}

func TestNoConfirmationGoesOutOnceTheWindowClosed(t *testing.T) {
	f := newFixture(t, storedLead("lead-1", leadNumber))
	f.promoShows("sair", sairConfirmation)
	f.now = stepNow.Add(25 * time.Hour)
	replier := &fakeReplier{}

	result, err := f.step.Run(context.Background(), afterPromo(typed("wamid.in", "/sair")), replier)

	if err != nil || !result.OptedOut || len(replier.sent) != 0 {
		t.Fatalf("Run = %+v, %v, sent %+v", result, err, replier.sent)
	}
	if run := f.runs.of("wamid.in"); run.Confirmation != leadexit.ConfirmationOutsideWindow {
		t.Fatalf("run = %+v", run)
	}
}

func TestAFailedConfirmationSendIsWrittenOnTheConversation(t *testing.T) {
	f := newFixture(t, storedLead("lead-1", leadNumber))
	f.promoShows("sair", sairConfirmation)

	result, err := f.step.Run(context.Background(), afterPromo(typed("wamid.in", "/sair")), &fakeReplier{err: errors.New("meta down")})

	if err != nil || !result.OptedOut {
		t.Fatalf("Run = %+v, %v", result, err)
	}
	if line := f.events.last(t); line["confirmation"] != "not_sent" || line["confirmation_reason"] != "lead_exit_confirmation_failed" {
		t.Fatalf("line = %+v", line)
	}
}

func TestALeadWithoutARecordHasNothingToRecord(t *testing.T) {
	f := newFixture(t)
	f.promoShows("sair", "")
	in := afterPromo(typed("wamid.in", "/sair"))
	in.LeadID = ""

	result, err := f.step.Run(context.Background(), in, &fakeReplier{})

	if err != nil || result.OptedOut || len(f.events.events) != 0 {
		t.Fatalf("Run = %+v, %v", result, err)
	}
}

func TestAnInboundWithoutItsWorkspaceOrIdIsRefused(t *testing.T) {
	f := newFixture(t)
	for _, in := range []leadexit.Inbound{{MessageID: "wamid.in", Text: "sair"}, {WorkspaceID: workspaceA, Text: "sair"}} {
		if _, err := f.step.Run(context.Background(), in, &fakeReplier{}); !errors.Is(err, leadexit.ErrInboundIncomplete) {
			t.Fatalf("%+v: %v", in, err)
		}
	}
}

func TestTheStepNeedsEveryDependency(t *testing.T) {
	if _, err := NewStep(Deps{}); !errors.Is(err, leadexit.ErrUnavailable) {
		t.Fatalf("got %v", err)
	}
}
