package lead

import (
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/recordevent"
)

var consentAt = time.Date(2026, 10, 16, 14, 0, 0, 0, time.UTC)

func operatorActor() ConsentActor { return ConsentActor{Type: ConsentActorUser, ID: "u-1"} }
func leadActor() ConsentActor     { return ConsentActor{Type: ConsentActorLead, ID: "l-1"} }
func agentActor() ConsentActor    { return ConsentActor{Type: ConsentActorAgent, ID: "ag-1"} }
func workflowActor() ConsentActor { return ConsentActor{Type: ConsentActorWorkflow, ID: "wf-1"} }
func systemActor() ConsentActor   { return ConsentActor{Type: ConsentActorSystem} }

func note(text string) ConsentEvidence {
	return ConsentEvidence{Type: EvidenceNote, Excerpt: text}
}

func message(ref string) ConsentEvidence {
	return ConsentEvidence{Type: EvidenceMessage, Ref: ref}
}

func spokenMessage(ref, excerpt string) ConsentEvidence {
	return ConsentEvidence{Type: EvidenceMessage, Ref: ref, Excerpt: excerpt}
}

func tap(ref string) ConsentEvidence {
	return ConsentEvidence{Type: EvidenceButtonReply, Ref: ref}
}

func change(kind ConsentKind, source ConsentSource, actor ConsentActor, evidence ConsentEvidence) ConsentChange {
	return ConsentChange{ID: "ev-1", Kind: kind, Source: source, Actor: actor, Evidence: evidence, At: consentAt}
}

func TestConsentRulesFollowTheSourceTable(t *testing.T) {
	cases := []struct {
		name   string
		change ConsentChange
		want   error
	}{
		{"an operator grants with a note", change(ConsentGrant, ConsentManual, operatorActor(), note("confirmou por telefone em 09/10")), nil},
		{"an operator grant without a note is refused", change(ConsentGrant, ConsentManual, operatorActor(), ConsentEvidence{}), ErrConsentEvidenceRequired},
		{"an operator grant citing a message instead of a note is refused", change(ConsentGrant, ConsentManual, operatorActor(), message("m-1")), ErrConsentEvidenceInvalid},
		{"an operator withdraws without evidence", change(ConsentWithdraw, ConsentManual, operatorActor(), ConsentEvidence{}), nil},
		{"an operator withdraws citing the lead's message", change(ConsentWithdraw, ConsentManual, operatorActor(), message("m-1")), nil},
		{"an operator source recorded by an agent is refused", change(ConsentGrant, ConsentManual, agentActor(), note("ok")), ErrConsentActorInvalid},

		{"a form grants with its submission", change(ConsentGrant, ConsentForm, systemActor(), ConsentEvidence{Type: EvidenceFormSubmission, Ref: "form-1:sub-9"}), nil},
		{"a form grant without its submission is refused", change(ConsentGrant, ConsentForm, systemActor(), ConsentEvidence{Type: EvidenceFormSubmission}), ErrConsentEvidenceRequired},
		{"a form never withdraws", change(ConsentWithdraw, ConsentForm, systemActor(), ConsentEvidence{Type: EvidenceFormSubmission, Ref: "form-1:sub-9"}), ErrConsentNotAllowed},

		{"an import grants with its line", change(ConsentGrant, ConsentImport, operatorActor(), ConsentEvidence{Type: EvidenceImportLine, Ref: "job-1:12"}), nil},
		{"an import grant without its line is refused", change(ConsentGrant, ConsentImport, operatorActor(), ConsentEvidence{Type: EvidenceNote, Excerpt: "planilha"}), ErrConsentEvidenceInvalid},
		{"an import never withdraws", change(ConsentWithdraw, ConsentImport, operatorActor(), ConsentEvidence{Type: EvidenceImportLine, Ref: "job-1:12"}), ErrConsentNotAllowed},

		{"a conversation request grants with the inbound message", change(ConsentGrant, ConsentConversationRequest, operatorActor(), message("m-1")), nil},
		{"a conversation request withdraws with the inbound message", change(ConsentWithdraw, ConsentConversationRequest, operatorActor(), message("m-1")), nil},
		{"a conversation request without the message is refused", change(ConsentWithdraw, ConsentConversationRequest, operatorActor(), ConsentEvidence{}), ErrConsentEvidenceRequired},

		{"a button tap grants with the tapped message", change(ConsentGrant, ConsentButtonReply, leadActor(), tap("wamid.in")), nil},
		{"a button tap withdraws with the tapped message", change(ConsentWithdraw, ConsentButtonReply, leadActor(), tap("wamid.in")), nil},
		{"a button tap without the tapped message is refused", change(ConsentWithdraw, ConsentButtonReply, leadActor(), ConsentEvidence{Type: EvidenceButtonReply}), ErrConsentEvidenceRequired},
		{"a button tap recorded by an operator is refused", change(ConsentWithdraw, ConsentButtonReply, operatorActor(), tap("wamid.in")), ErrConsentActorInvalid},

		{"a workflow grants from the reply that triggered it", change(ConsentGrant, ConsentWorkflow, workflowActor(), message("m-1")), nil},
		{"a workflow grants from a button reply", change(ConsentGrant, ConsentWorkflow, workflowActor(), tap("wamid.in")), nil},
		{"a workflow grant from a note is refused", change(ConsentGrant, ConsentWorkflow, workflowActor(), note("fluxo de boas-vindas")), ErrConsentEvidenceInvalid},
		{"a workflow withdraws from a note", change(ConsentWithdraw, ConsentWorkflow, workflowActor(), note("fluxo de saída")), nil},
		{"a workflow withdrawal without evidence is refused", change(ConsentWithdraw, ConsentWorkflow, workflowActor(), ConsentEvidence{}), ErrConsentEvidenceRequired},

		{"an agent withdraws with the lead's words", change(ConsentWithdraw, ConsentAIAgent, agentActor(), spokenMessage("m-1", "não quero mais receber isso")), nil},
		{"an agent grants with the lead's yes", change(ConsentGrant, ConsentAIAgent, agentActor(), spokenMessage("m-2", "sim, pode mandar")), nil},
		{"an agent withdrawal without the excerpt is refused", change(ConsentWithdraw, ConsentAIAgent, agentActor(), message("m-1")), ErrConsentEvidenceRequired},
		{"an agent source recorded by a workflow is refused", change(ConsentWithdraw, ConsentAIAgent, workflowActor(), spokenMessage("m-1", "pare")), ErrConsentActorInvalid},

		{"Elo grants with the operator's approval note", change(ConsentGrant, ConsentElo, operatorActor(), note("aprovado no chat")), nil},
		{"Elo withdraws with the operator's approval note", change(ConsentWithdraw, ConsentElo, operatorActor(), note("aprovado no chat")), nil},
		{"Elo without a note is refused", change(ConsentGrant, ConsentElo, operatorActor(), ConsentEvidence{}), ErrConsentEvidenceRequired},

		{"a keyword withdraws with the inbound message", change(ConsentWithdraw, ConsentKeyword, leadActor(), message("wamid.in")), nil},
		{"a keyword never grants", change(ConsentGrant, ConsentKeyword, leadActor(), message("wamid.in")), ErrConsentNotAllowed},
		{"a keyword without the message is refused", change(ConsentWithdraw, ConsentKeyword, leadActor(), ConsentEvidence{}), ErrConsentEvidenceRequired},

		{"the API grants with a caller ref", change(ConsentGrant, ConsentAPI, operatorActor(), ConsentEvidence{Type: EvidenceFormSubmission, Ref: "crm-77"}), nil},
		{"the API withdraws with a caller ref", change(ConsentWithdraw, ConsentAPI, systemActor(), ConsentEvidence{Type: EvidenceNote, Ref: "crm-77"}), nil},
		{"the API without a ref is refused", change(ConsentGrant, ConsentAPI, operatorActor(), ConsentEvidence{Type: EvidenceNote}), ErrConsentEvidenceRequired},

		{"an unknown source is refused", change(ConsentGrant, "rumour", operatorActor(), note("x")), ErrConsentSourceUnknown},
		{"an unknown kind is refused", change("maybe", ConsentManual, operatorActor(), note("x")), ErrConsentKindInvalid},
		{"a missing actor is refused", change(ConsentWithdraw, ConsentManual, ConsentActor{}, ConsentEvidence{}), ErrConsentActorInvalid},
		{"a user actor without an id is refused", change(ConsentWithdraw, ConsentManual, ConsentActor{Type: ConsentActorUser}, ConsentEvidence{}), ErrConsentActorInvalid},
		{"an unknown evidence type is refused", change(ConsentWithdraw, ConsentManual, operatorActor(), ConsentEvidence{Type: "rumour", Ref: "x"}), ErrConsentEvidenceInvalid},
		{"a change without a time is refused", ConsentChange{Kind: ConsentWithdraw, Source: ConsentManual, Actor: operatorActor()}, ErrConsentTimeRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ConsentRules(tc.change); !errors.Is(got, tc.want) {
				t.Fatalf("ConsentRules = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestConsentRulesBoundTheExcerptAndThePurpose(t *testing.T) {
	long := change(ConsentWithdraw, ConsentAIAgent, agentActor(), spokenMessage("m-1", strings.Repeat("a", MaxConsentExcerptLength+1)))
	if err := ConsentRules(long); !errors.Is(err, ErrConsentExcerptTooLong) {
		t.Fatalf("a 281-character excerpt = %v, want ErrConsentExcerptTooLong", err)
	}
	fits := change(ConsentWithdraw, ConsentAIAgent, agentActor(), spokenMessage("m-1", strings.Repeat("ã", MaxConsentExcerptLength)))
	if err := ConsentRules(fits); err != nil {
		t.Fatalf("a 280-character excerpt = %v, want it accepted", err)
	}
	wordy := change(ConsentGrant, ConsentManual, operatorActor(), note("ok"))
	wordy.Purpose = strings.Repeat("p", MaxConsentPurposeLength+1)
	if err := ConsentRules(wordy); !errors.Is(err, ErrConsentPurposeTooLong) {
		t.Fatalf("a long purpose = %v, want ErrConsentPurposeTooLong", err)
	}
}

func TestEveryConsentRefusalIsCodedAsAnInputRefusal(t *testing.T) {
	codes := map[error]string{
		ErrConsentSourceUnknown:    "lead_consent_source_unknown",
		ErrConsentKindInvalid:      "lead_consent_kind_invalid",
		ErrConsentNotAllowed:       "lead_consent_not_allowed",
		ErrConsentActorInvalid:     "lead_consent_actor_invalid",
		ErrConsentEvidenceRequired: "lead_consent_evidence_required",
		ErrConsentEvidenceInvalid:  "lead_consent_evidence_invalid",
		ErrConsentExcerptTooLong:   "lead_consent_excerpt_too_long",
		ErrConsentPurposeTooLong:   "lead_consent_purpose_too_long",
		ErrConsentTimeRequired:     "lead_consent_time_required",
	}
	for err, code := range codes {
		if ErrorCode(err) != code || !IsInputRefusal(err) {
			t.Fatalf("%v: code %q input %v, want %q and an input refusal", err, ErrorCode(err), IsInputRefusal(err), code)
		}
	}
	for err, code := range map[error]string{
		ErrLeadOptedOut:             "lead_opted_out",
		ErrOptOutConfirmationStale:  "lead_opt_out_confirmation_stale",
		ErrConsentSourceForbidden:   "lead_consent_source_forbidden",
		ErrLeadConsentSourceInvalid: "lead_consent_source_invalid",
		ErrLeadOptOutSourceInvalid:  "lead_opt_out_source_invalid",
	} {
		if ErrorCode(err) != code {
			t.Fatalf("%v: code %q, want %q", err, ErrorCode(err), code)
		}
	}
}

func TestEveryNewSourceIsAKnownConsentSource(t *testing.T) {
	for _, source := range []ConsentSource{ConsentForm, ConsentImport, ConsentManual, ConsentConversationRequest,
		ConsentButtonReply, ConsentWorkflow, ConsentAIAgent, ConsentElo, ConsentKeyword, ConsentAPI} {
		if !source.Valid() {
			t.Fatalf("%q must be a valid consent source", source)
		}
	}
	if ConsentSource("rumour").Valid() {
		t.Fatal("an unknown source must stay invalid")
	}
}

func grantedLead() *Lead {
	return &Lead{ID: "l-1", WorkspaceID: "ws-1", Number: "5511987654321",
		WhatsAppOptIn: &Consent{GrantedAt: consentAt.Add(-time.Hour), Source: ConsentForm, Purpose: "novidades"}}
}

func TestAWithdrawalOptsTheLeadOutAndClearsTheGrant(t *testing.T) {
	l := grantedLead()
	c := change(ConsentWithdraw, ConsentKeyword, leadActor(), ConsentEvidence{Type: EvidenceMessage, Ref: "wamid.in", Excerpt: "SAIR"})

	changed, err := l.ApplyConsentChange(c)

	if err != nil || !changed {
		t.Fatalf("ApplyConsentChange = (%v, %v), want a change", changed, err)
	}
	if l.OptedOutAt == nil || !l.OptedOutAt.Equal(consentAt) || l.WhatsAppOptIn != nil || l.OptOutSource != OptOutLeadRequest {
		t.Fatalf("lead after the withdrawal = %+v", l)
	}
	record := l.ConsentRecord
	if record == nil || record.Kind != ConsentWithdraw || record.Source != ConsentKeyword || record.Actor != leadActor() ||
		record.Evidence.Ref != "wamid.in" || record.Evidence.Excerpt != "SAIR" || record.EventID != "ev-1" || !record.At.Equal(consentAt) {
		t.Fatalf("consent record = %+v", record)
	}
}

func TestWithdrawingAnAlreadyWithdrawnLeadChangesNothing(t *testing.T) {
	l := grantedLead()
	if _, err := l.ApplyConsentChange(change(ConsentWithdraw, ConsentManual, operatorActor(), ConsentEvidence{})); err != nil {
		t.Fatal(err)
	}
	first := *l
	again := change(ConsentWithdraw, ConsentKeyword, leadActor(), message("wamid.2"))
	again.ID, again.At = "ev-2", consentAt.Add(time.Hour)

	changed, err := l.ApplyConsentChange(again)

	if err != nil || changed {
		t.Fatalf("a second withdrawal = (%v, %v), want no change", changed, err)
	}
	if !l.OptedOutAt.Equal(*first.OptedOutAt) || l.OptOutSource != OptOutOperator || l.ConsentRecord.EventID != "ev-1" {
		t.Fatalf("the first withdrawal must stay: %+v %+v", l, l.ConsentRecord)
	}
}

func TestTheStoredOptOutSourceSaysWhetherTheLeadAsked(t *testing.T) {
	cases := []struct {
		name   string
		change ConsentChange
		want   OptOutSource
		err    error
	}{
		{"an operator decision", change(ConsentWithdraw, ConsentManual, operatorActor(), ConsentEvidence{}), OptOutOperator, nil},
		{"an operator relaying the lead's request", withRequest(change(ConsentWithdraw, ConsentManual, operatorActor(), ConsentEvidence{}), OptOutLeadRequest), OptOutLeadRequest, nil},
		{"a keyword", change(ConsentWithdraw, ConsentKeyword, leadActor(), message("m")), OptOutLeadRequest, nil},
		{"a button tap", change(ConsentWithdraw, ConsentButtonReply, leadActor(), tap("m")), OptOutLeadRequest, nil},
		{"an agent citing the lead", change(ConsentWithdraw, ConsentAIAgent, agentActor(), spokenMessage("m", "pare")), OptOutLeadRequest, nil},
		{"a conversation request", change(ConsentWithdraw, ConsentConversationRequest, operatorActor(), message("m")), OptOutLeadRequest, nil},
		{"a workflow on its own", change(ConsentWithdraw, ConsentWorkflow, workflowActor(), note("limpeza")), OptOutOperator, nil},
		{"a workflow relaying the lead's reply", withRequest(change(ConsentWithdraw, ConsentWorkflow, workflowActor(), message("m")), OptOutLeadRequest), OptOutLeadRequest, nil},
		{"a lead's own request claimed as the team's", withRequest(change(ConsentWithdraw, ConsentKeyword, leadActor(), message("m")), OptOutOperator), "", ErrLeadOptOutSourceInvalid},
		{"an unknown requester", withRequest(change(ConsentWithdraw, ConsentManual, operatorActor(), ConsentEvidence{}), "rumour"), "", ErrLeadOptOutSourceInvalid},
		{"a grant naming a requester", withRequest(change(ConsentGrant, ConsentManual, operatorActor(), note("ok")), OptOutOperator), "", ErrLeadOptOutSourceInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := grantedLead()
			_, err := l.ApplyConsentChange(tc.change)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err = %v, want %v", err, tc.err)
			}
			if tc.err == nil && (l.OptOutSource != tc.want || !l.OptOutSource.Valid()) {
				t.Fatalf("stored source = %q, want %q", l.OptOutSource, tc.want)
			}
		})
	}
}

func withRequest(c ConsentChange, requested OptOutSource) ConsentChange {
	c.RequestedBy = requested
	return c
}

func optedOutLead() *Lead {
	l := &Lead{ID: "l-1", WorkspaceID: "ws-1", Number: "5511987654321"}
	if _, err := l.ApplyConsentChange(change(ConsentWithdraw, ConsentKeyword, leadActor(), message("wamid.in"))); err != nil {
		panic(err)
	}
	return l
}

func TestAnImportNeverGrantsOverAnOptOut(t *testing.T) {
	l := optedOutLead()
	grant := change(ConsentGrant, ConsentImport, operatorActor(), ConsentEvidence{Type: EvidenceImportLine, Ref: "job-1:4"})
	grant.At = consentAt.Add(time.Hour)

	changed, err := l.ApplyConsentChange(grant)

	if !errors.Is(err, ErrLeadOptedOut) || changed || l.OptedOutAt == nil || l.WhatsAppOptIn != nil {
		t.Fatalf("an import over an opt-out = (%v, %v), lead %+v", changed, err, l)
	}
}

func TestAnExplicitGrantWithEvidenceUndoesAnOptOut(t *testing.T) {
	for _, grant := range []ConsentChange{
		change(ConsentGrant, ConsentManual, operatorActor(), note("pediu para voltar a receber")),
		change(ConsentGrant, ConsentButtonReply, leadActor(), tap("wamid.yes")),
		change(ConsentGrant, ConsentForm, systemActor(), ConsentEvidence{Type: EvidenceFormSubmission, Ref: "f:1"}),
	} {
		l := optedOutLead()
		grant.At, grant.Purpose = consentAt.Add(time.Hour), "novidades"

		changed, err := l.ApplyConsentChange(grant)

		if err != nil || !changed || l.OptedOutAt != nil || l.OptOutSource != "" || l.WhatsAppOptIn == nil ||
			l.WhatsAppOptIn.Source != grant.Source || l.WhatsAppOptIn.Purpose != "novidades" || !l.WhatsAppOptIn.GrantedAt.Equal(grant.At) {
			t.Fatalf("%s grant = (%v, %v), lead %+v", grant.Source, changed, err, l)
		}
		if l.ConsentRecord.Kind != ConsentGrant || l.ConsentRecord.Source != grant.Source {
			t.Fatalf("the record must describe the grant: %+v", l.ConsentRecord)
		}
	}
}

func TestGrantingAGrantedLeadKeepsTheFirstGrant(t *testing.T) {
	l := grantedLead()
	first := *l.WhatsAppOptIn

	changed, err := l.ApplyConsentChange(change(ConsentGrant, ConsentManual, operatorActor(), note("de novo")))

	if err != nil || changed || *l.WhatsAppOptIn != first || l.ConsentRecord != nil {
		t.Fatalf("a second grant = (%v, %v), consent %+v record %+v", changed, err, l.WhatsAppOptIn, l.ConsentRecord)
	}
}

func TestARefusedChangeLeavesTheLeadAsItWas(t *testing.T) {
	l := grantedLead()
	before := *l

	changed, err := l.ApplyConsentChange(change(ConsentGrant, ConsentKeyword, leadActor(), message("m")))

	if !errors.Is(err, ErrConsentNotAllowed) || changed || l.WhatsAppOptIn != before.WhatsAppOptIn || l.OptedOutAt != nil {
		t.Fatalf("a refused change = (%v, %v), lead %+v", changed, err, l)
	}
}

func TestTheConsentEventCarriesWhoWhyAndTheRefButNeverTheExcerpt(t *testing.T) {
	before := grantedLead()
	after := *before
	c := change(ConsentWithdraw, ConsentAIAgent, agentActor(), spokenMessage("m-9", "não quero mais"))
	c.Purpose = "novidades"
	if _, err := after.ApplyConsentChange(c); err != nil {
		t.Fatal(err)
	}

	event := ConsentEvent(c, before, &after)

	if event.ID != "ev-1" || event.Kind != EventConsentWithdrawn || event.Actor != "ai:ag-1" {
		t.Fatalf("event = %+v", event)
	}
	detail := consentDetailOf(t, event)
	want := map[string]any{"kind": "withdraw", "source": "ai_agent", "actorType": "agent", "actorId": "ag-1",
		"purpose": "novidades", "evidenceType": "message", "evidenceRef": "m-9"}
	for key, value := range want {
		if detail[key] != value {
			t.Fatalf("detail[%s] = %v, want %v (detail %v)", key, detail[key], value, detail)
		}
	}
	for _, c := range event.Changes {
		if strings.Contains(stringOf(c.Before)+stringOf(c.After), "não quero mais") {
			t.Fatalf("the excerpt leaked into the event: %+v", c)
		}
	}
	fields := event.Fields()
	for _, field := range []string{FieldConsent, FieldOptedOut, FieldOptOutSource, FieldWhatsAppOptIn} {
		if !contains(fields, field) {
			t.Fatalf("event fields %v lack %s", fields, field)
		}
	}
}

func TestAGrantEventIsAGrantByTheLeadAsSystem(t *testing.T) {
	before := optedOutLead()
	after := *before
	c := change(ConsentGrant, ConsentButtonReply, leadActor(), tap("wamid.yes"))
	if _, err := after.ApplyConsentChange(c); err != nil {
		t.Fatal(err)
	}

	event := ConsentEvent(c, before, &after)

	if event.Kind != EventConsentGranted || event.Actor != "system" || consentDetailOf(t, event)["actorType"] != "lead" {
		t.Fatalf("event = %+v", event)
	}
}

func consentDetailOf(t *testing.T, e recordevent.Event) map[string]any {
	t.Helper()
	for _, c := range e.Changes {
		if c.Field == FieldConsent {
			detail, ok := c.After.(map[string]any)
			if !ok {
				t.Fatalf("consent detail = %#v", c.After)
			}
			return detail
		}
	}
	t.Fatalf("no consent change in %+v", e.Changes)
	return nil
}

func stringOf(v any) string {
	switch value := v.(type) {
	case string:
		return value
	case map[string]any:
		var parts []string
		for _, inner := range value {
			parts = append(parts, stringOf(inner))
		}
		return strings.Join(parts, " ")
	}
	return ""
}

func contains(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func TestTheOptOutConfirmationIsAllowedOncePerWithdrawal(t *testing.T) {
	l := optedOutLead()

	if err := l.ClaimOptOutConfirmation("ev-1", consentAt.Add(time.Minute)); err != nil {
		t.Fatalf("the first confirmation = %v, want it allowed", err)
	}
	if l.ConsentRecord.ConfirmationSentAt == nil || !l.ConsentRecord.ConfirmationSentAt.Equal(consentAt.Add(time.Minute)) {
		t.Fatalf("the confirmation must be stamped: %+v", l.ConsentRecord)
	}
	if err := l.ClaimOptOutConfirmation("ev-1", consentAt.Add(2*time.Minute)); !errors.Is(err, ErrLeadOptedOut) {
		t.Fatalf("a second confirmation = %v, want ErrLeadOptedOut", err)
	}
}

func TestTheOptOutConfirmationBelongsToTheCurrentWithdrawal(t *testing.T) {
	l := optedOutLead()
	if err := l.ClaimOptOutConfirmation("ev-older", consentAt); !errors.Is(err, ErrLeadOptedOut) {
		t.Fatalf("another withdrawal's confirmation = %v, want ErrLeadOptedOut", err)
	}
	if err := l.ClaimOptOutConfirmation("", consentAt); !errors.Is(err, ErrOptOutConfirmationStale) {
		t.Fatalf("a confirmation without its withdrawal = %v, want ErrOptOutConfirmationStale", err)
	}
	regranted := optedOutLead()
	if _, err := regranted.ApplyConsentChange(change(ConsentGrant, ConsentButtonReply, leadActor(), tap("wamid.yes"))); err != nil {
		t.Fatal(err)
	}
	if err := regranted.ClaimOptOutConfirmation("ev-1", consentAt); !errors.Is(err, ErrOptOutConfirmationStale) {
		t.Fatalf("a confirmation after a new grant = %v, want ErrOptOutConfirmationStale", err)
	}
	if ErrorCode(ErrOptOutConfirmationStale) != "lead_opt_out_confirmation_stale" {
		t.Fatalf("code = %q", ErrorCode(ErrOptOutConfirmationStale))
	}
}

func TestAClaimedConfirmationDoesNotChangeTheEarlierCopy(t *testing.T) {
	l := optedOutLead()
	kept := l.ConsentRecord
	if err := l.ClaimOptOutConfirmation("ev-1", consentAt); err != nil {
		t.Fatal(err)
	}
	if kept.ConfirmationSentAt != nil {
		t.Fatal("claiming must not write through a shared record")
	}
}

func TestChannelDataMergedIntoAnOptedOutLeadNeverGrants(t *testing.T) {
	l := optedOutLead()
	record := l.ConsentRecord

	l.MergeIncoming(LeadUpdate{Source: SourceChannel, Name: "Ana", ProfilePictureURL: "https://x/ana.png"})

	if l.OptedOutAt == nil || l.WhatsAppOptIn != nil || l.ConsentRecord != record {
		t.Fatalf("a channel merge touched the opt-out: %+v", l)
	}
}

func TestAnUntickedSheetIsTheOperatorsWithdrawalAndAnEmptyOneChangesNothing(t *testing.T) {
	untouched := Lead{WorkspaceID: "ws-1", Number: "5511987654321"}
	no := false
	if err := untouched.ApplyEdit(Edit{WhatsAppOptIn: &no, ConsentBy: "u-1"}, consentAt); err != nil || untouched.OptedOutAt != nil {
		t.Fatalf("unticking a lead with no grant must not opt it out: %+v, %v", untouched, err)
	}
	granted := grantedLead()
	if err := granted.ApplyEdit(Edit{WhatsAppOptIn: &no, ConsentBy: "u-1"}, consentAt); err != nil {
		t.Fatal(err)
	}
	if granted.OptedOutAt == nil || granted.OptOutSource != OptOutOperator || granted.ConsentRecord.Actor.ID != "u-1" {
		t.Fatalf("unticking a grant = %+v %+v", granted, granted.ConsentRecord)
	}
}
