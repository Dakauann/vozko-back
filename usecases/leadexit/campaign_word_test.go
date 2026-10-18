package leadexit_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/lead"
	"vozko/domain/leadexit"
)

const unofficialEntry = "unofficial_whatsapp"

func unofficialTyped(messageID, text string) leadexit.Inbound {
	return leadexit.Inbound{
		WorkspaceID: workspaceA, LeadID: "lead-1", SenderNumber: leadNumber, EntryID: "conv-1", EntryType: unofficialEntry, Channel: unofficialEntry,
		MessageID: messageID, Text: text, ReceivedAt: stepNow,
	}
}

func (f *fixture) campaignSent(text string) {
	f.campaigns.last[unofficialEntry+"|conv-1"] = &leadexit.SentCampaignMessage{MessageID: "msg-camp", CampaignID: "camp-u", Text: text}
}

func TestTheSairShownInTheLastCampaignMessageOptsTheLeadOutWithoutAConfirmation(t *testing.T) {
	f := newFixture(t, storedLead("lead-1", leadNumber))
	f.campaignSent("Oi Ana! Responda /sair para não receber mais mensagens.")
	replier := &fakeReplier{}

	result, err := f.step.Run(context.Background(), unofficialTyped("prov-in", "/Sair"), replier)

	if err != nil || !result.OptedOut {
		t.Fatalf("Run = %+v, %v", result, err)
	}
	stored := f.leads.get(t, "lead-1")
	record := stored.ConsentRecord
	if stored.OptedOutAt == nil || record == nil || record.Kind != lead.ConsentWithdraw || record.Source != lead.ConsentKeyword ||
		record.Evidence.Type != lead.EvidenceMessage || record.Evidence.Ref != "prov-in" || record.Evidence.Excerpt != "/Sair" {
		t.Fatalf("record = %+v", record)
	}
	if len(replier.sent) != 0 {
		t.Fatalf("an unofficial campaign has no confirmation, sent %+v", replier.sent)
	}
	line := f.events.last(t)
	if line["trigger"] != "campaign_word" || line["word"] != "sair" || line["campaign_id"] != "camp-u" || line["confirmation"] != "none" {
		t.Fatalf("line = %+v", line)
	}
	if run := f.runs.of("prov-in"); run.Trigger != leadexit.TriggerCampaignWord || run.Outcome != leadexit.OutcomeChanged {
		t.Fatalf("run = %+v", run)
	}
}

func TestACommandTheCampaignMessageDidNotShowLeavesTheLeadAlone(t *testing.T) {
	f := newFixture(t, storedLead("lead-1", leadNumber))
	f.campaignSent("Oi Ana, temos novidades para você.")

	result, err := f.step.Run(context.Background(), unofficialTyped("prov-in", "/sair"), &fakeReplier{})

	if err != nil || result.OptedOut || f.leads.saves != 0 {
		t.Fatalf("Run = %+v, %v, saves %d", result, err, f.leads.saves)
	}
}

func TestWithoutACampaignMessageNoWordCounts(t *testing.T) {
	f := newFixture(t, storedLead("lead-1", leadNumber))

	result, err := f.step.Run(context.Background(), unofficialTyped("prov-in", "/sair"), &fakeReplier{})

	if err != nil || result.OptedOut || f.leads.saves != 0 {
		t.Fatalf("Run = %+v, %v, saves %d", result, err, f.leads.saves)
	}
}

func TestAFailedCampaignMessageLookupFailsTheStep(t *testing.T) {
	f := newFixture(t, storedLead("lead-1", leadNumber))
	f.campaigns.err = errStoreDown

	result, err := f.step.Run(context.Background(), unofficialTyped("prov-in", "/sair"), &fakeReplier{})

	if !errors.Is(err, errStoreDown) || result.OptedOut || f.leads.saves != 0 {
		t.Fatalf("Run = %+v, %v, saves %d", result, err, f.leads.saves)
	}
}

func TestOnlyACommandLooksUpTheCampaignMessage(t *testing.T) {
	f := newFixture(t, storedLead("lead-1", leadNumber))
	f.campaigns.err = errStoreDown

	result, err := f.step.Run(context.Background(), unofficialTyped("prov-in", "quero saber o preço"), &fakeReplier{})

	if err != nil || result.OptedOut || len(f.campaigns.calls) != 0 {
		t.Fatalf("Run = %+v, %v, lookups %v", result, err, f.campaigns.calls)
	}
}

func TestTheStepNeedsTheCampaignMessages(t *testing.T) {
	f := newFixture(t)
	deps := f.step.deps
	deps.Campaigns = nil
	if _, err := NewStep(deps); !errors.Is(err, leadexit.ErrUnavailable) {
		t.Fatalf("got %v", err)
	}
}
