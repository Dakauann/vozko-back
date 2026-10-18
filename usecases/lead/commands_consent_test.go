package lead_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/lead"
)

func consentPermissions() fakePermissions {
	perms := allPermissions()
	perms["leads:consent"] = true
	return perms
}

func operatorGrant() ConsentInput {
	return ConsentInput{Kind: lead.ConsentGrant, Source: lead.ConsentManual, Purpose: "novidades",
		Evidence: lead.ConsentEvidence{Type: lead.EvidenceNote, Excerpt: "confirmou por telefone em 09/10"}}
}

func TestAnOperatorGrantNeedsTheConsentPermission(t *testing.T) {
	f := newCommandsFixture(t, allPermissions(), storedLead())
	if _, err := f.cmds.RecordConsent(context.Background(), operator(), "l-1", operatorGrant()); !errors.Is(err, lead.ErrLeadForbidden) {
		t.Fatalf("a grant without leads:consent = %v, want forbidden", err)
	}
	if len(f.store.saves) != 0 {
		t.Fatal("a forbidden grant writes nothing")
	}
}

func TestAnOperatorGrantIsRecordedAsTheOperatorWithTheirNote(t *testing.T) {
	f := newCommandsFixture(t, consentPermissions(), storedLead())

	got, err := f.cmds.RecordConsent(context.Background(), operator(), "l-1", operatorGrant())

	if err != nil || !got.Changed || got.EventID == "" || got.Lead.WhatsAppOptIn == nil || got.Lead.WhatsAppOptIn.Purpose != "novidades" {
		t.Fatalf("RecordConsent = %+v, %v", got, err)
	}
	stored := f.store.leads["l-1"].ConsentRecord
	if stored == nil || stored.Actor != (lead.ConsentActor{Type: lead.ConsentActorUser, ID: cmdUser}) || stored.Evidence.Excerpt == "" {
		t.Fatalf("record = %+v", stored)
	}
	if event := f.store.saves[0].events[0]; event.Kind != lead.EventConsentGranted || event.Actor != cmdUser || event.ID != got.EventID {
		t.Fatalf("event = %+v", event)
	}
}

func TestAWithdrawalNeedsOnlyLeadsUpdate(t *testing.T) {
	f := newCommandsFixture(t, allPermissions(), storedLead())

	got, err := f.cmds.RecordConsent(context.Background(), operator(), "l-1", ConsentInput{Kind: lead.ConsentWithdraw, Source: lead.ConsentManual})

	if err != nil || !got.Changed || got.Lead.OptedOutAt == nil || got.Lead.OptOutSource != lead.OptOutOperator {
		t.Fatalf("RecordConsent = %+v, %v", got, err)
	}
	readOnly := newCommandsFixture(t, fakePermissions{"leads:read": true}, storedLead())
	if _, err := readOnly.cmds.RecordConsent(context.Background(), operator(), "l-1", ConsentInput{Kind: lead.ConsentWithdraw, Source: lead.ConsentManual}); !errors.Is(err, lead.ErrLeadForbidden) {
		t.Fatalf("a withdrawal without leads:update = %v, want forbidden", err)
	}
}

func TestAnOperatorCannotClaimTheLeadsOrAnAutomationsSource(t *testing.T) {
	for _, source := range []lead.ConsentSource{lead.ConsentKeyword, lead.ConsentButtonReply, lead.ConsentAIAgent, lead.ConsentWorkflow, lead.ConsentElo, lead.ConsentForm, lead.ConsentImport} {
		f := newCommandsFixture(t, consentPermissions(), storedLead())
		in := ConsentInput{Kind: lead.ConsentWithdraw, Source: source, Evidence: lead.ConsentEvidence{Type: lead.EvidenceMessage, Ref: "m-1", Excerpt: "pare"}}
		if _, err := f.cmds.RecordConsent(context.Background(), operator(), "l-1", in); !errors.Is(err, lead.ErrConsentSourceForbidden) {
			t.Fatalf("%s from an operator = %v, want ErrConsentSourceForbidden", source, err)
		}
	}
	if lead.ErrorCode(lead.ErrConsentSourceForbidden) != "lead_consent_source_forbidden" {
		t.Fatal("the refusal is coded")
	}
}

func TestAnOperatorRecordsAConversationRequestAndAnAPIRef(t *testing.T) {
	for _, in := range []ConsentInput{
		{Kind: lead.ConsentWithdraw, Source: lead.ConsentConversationRequest, Evidence: lead.ConsentEvidence{Type: lead.EvidenceMessage, Ref: "m-1"}},
		{Kind: lead.ConsentGrant, Source: lead.ConsentAPI, Evidence: lead.ConsentEvidence{Type: lead.EvidenceFormSubmission, Ref: "crm-9"}},
	} {
		f := newCommandsFixture(t, consentPermissions(), storedLead())
		if got, err := f.cmds.RecordConsent(context.Background(), operator(), "l-1", in); err != nil || !got.Changed {
			t.Fatalf("%s = %+v, %v", in.Source, got, err)
		}
	}
}

func TestTheSheetToggleGrantsOnlyWithTheConsentPermission(t *testing.T) {
	yes := true
	f := newCommandsFixture(t, allPermissions(), storedLead())
	if _, err := f.cmds.Update(context.Background(), operator(), "l-1", version(3), lead.Edit{WhatsAppOptIn: &yes}); !errors.Is(err, lead.ErrLeadForbidden) {
		t.Fatalf("ticking consent without leads:consent = %v, want forbidden", err)
	}
	if _, err := f.cmds.Create(context.Background(), operator(), lead.Draft{Name: "Bia", WhatsAppOptIn: &yes}); !errors.Is(err, lead.ErrLeadForbidden) {
		t.Fatalf("creating a consenting lead without leads:consent = %v, want forbidden", err)
	}

	allowed := newCommandsFixture(t, consentPermissions(), storedLead())
	got, err := allowed.cmds.Update(context.Background(), operator(), "l-1", version(3), lead.Edit{WhatsAppOptIn: &yes})
	if err != nil || got.WhatsAppOptIn == nil {
		t.Fatalf("Update = %+v, %v", got, err)
	}
	if record := allowed.store.leads["l-1"].ConsentRecord; record == nil || record.Actor.ID != cmdUser || record.Evidence.Ref != lead.SheetEvidenceRef {
		t.Fatalf("the sheet grant must name who ticked it: %+v", record)
	}
}

func TestUntickingTheSheetIsAWithdrawalAnyEditorMayRecord(t *testing.T) {
	no := false
	granted := storedLead()
	granted.WhatsAppOptIn = &lead.Consent{GrantedAt: cmdNow, Source: lead.ConsentForm}
	f := newCommandsFixture(t, allPermissions(), granted)

	got, err := f.cmds.Update(context.Background(), operator(), "l-1", version(3), lead.Edit{WhatsAppOptIn: &no})

	if err != nil || got.WhatsAppOptIn != nil || got.OptedOutAt == nil || got.OptOutSource != lead.OptOutOperator {
		t.Fatalf("Update = %+v, %v", got, err)
	}
}
