package lead

import (
	"errors"
	"testing"
	"time"
)

var revertAt = consentAt.Add(48 * time.Hour)

func leadWithAGrant() *Lead {
	l := &Lead{ID: "l-1", WorkspaceID: "ws-1"}
	grant := change(ConsentGrant, ConsentManual, operatorActor(), note("aceitou no balcão"))
	grant.ID, grant.Purpose = "ev-grant", "novidades"
	if _, err := l.ApplyConsentChange(grant); err != nil {
		panic(err)
	}
	return l
}

func withdrawThrough(l *Lead, source ConsentSource, actor ConsentActor, evidence ConsentEvidence) *Lead {
	withdrawal := change(ConsentWithdraw, source, actor, evidence)
	withdrawal.ID, withdrawal.At = "ev-out", consentAt.Add(time.Hour)
	if _, err := l.ApplyConsentChange(withdrawal); err != nil {
		panic(err)
	}
	return l
}

func revertOf(eventID string) ConsentChange {
	c := RevertWithdrawal("u-9", "  o botão SAIR estava ligado à opção errada  ", eventID, revertAt)
	c.ID = "ev-revert"
	return c
}

func TestAWithdrawalKeepsTheGrantItReplacedSoItCanBeRestored(t *testing.T) {
	l := withdrawThrough(leadWithAGrant(), ConsentButtonReply, leadActor(), tap("wamid.in"))
	if l.ConsentRecord.PriorGrant == nil || l.ConsentRecord.PriorGrant.Source != ConsentManual || l.ConsentRecord.PriorGrant.Purpose != "novidades" {
		t.Fatalf("record = %+v", l.ConsentRecord)
	}
}

func TestRevertingAnAutomatedWithdrawalRestoresTheStateBeforeIt(t *testing.T) {
	for _, source := range []struct {
		source   ConsentSource
		actor    ConsentActor
		evidence ConsentEvidence
	}{
		{ConsentButtonReply, leadActor(), tap("wamid.in")},
		{ConsentKeyword, leadActor(), message("m-1")},
		{ConsentAIAgent, agentActor(), spokenMessage("m-1", "não quero mais")},
		{ConsentWorkflow, workflowActor(), note("fluxo de saída")},
	} {
		t.Run(string(source.source), func(t *testing.T) {
			l := withdrawThrough(leadWithAGrant(), source.source, source.actor, source.evidence)
			changed, err := l.ApplyConsentChange(revertOf("ev-out"))
			if err != nil || !changed {
				t.Fatalf("revert = %v, %v", changed, err)
			}
			if l.OptedOutAt != nil || l.OptOutSource != "" {
				t.Fatalf("still opted out: %+v", l)
			}
			if l.WhatsAppOptIn == nil || l.WhatsAppOptIn.Source != ConsentManual || !l.WhatsAppOptIn.GrantedAt.Equal(consentAt) {
				t.Fatalf("the grant before the withdrawal was not restored: %+v", l.WhatsAppOptIn)
			}
			record := l.ConsentRecord
			if record.Kind != ConsentRevertWithdrawal || record.Source != source.source || record.Reverts != "ev-out" || record.EventID != "ev-revert" ||
				record.Actor != (ConsentActor{Type: ConsentActorUser, ID: "u-9"}) || record.Evidence.Type != EvidenceNote ||
				record.Evidence.Excerpt != "o botão SAIR estava ligado à opção errada" || !record.At.Equal(revertAt) {
				t.Fatalf("record = %+v", record)
			}
		})
	}
}

func TestRevertingAWithdrawalWithoutAnEarlierGrantLeavesNoGrant(t *testing.T) {
	l := withdrawThrough(&Lead{ID: "l-1"}, ConsentKeyword, leadActor(), message("m-1"))
	if changed, err := l.ApplyConsentChange(revertOf("ev-out")); err != nil || !changed {
		t.Fatalf("revert = %v, %v", changed, err)
	}
	if l.OptedOutAt != nil || l.WhatsAppOptIn != nil {
		t.Fatalf("lead = %+v", l)
	}
}

func TestOnlyAnAutomatedWithdrawalThatIsStillCurrentCanBeReverted(t *testing.T) {
	cases := []struct {
		name string
		lead func() *Lead
		id   string
		want error
	}{
		{"an operator's opt-out", func() *Lead {
			return withdrawThrough(leadWithAGrant(), ConsentManual, operatorActor(), ConsentEvidence{})
		}, "ev-out", ErrOptOutNotRestorable},
		{"an opt-out an agent heard in a conversation request", func() *Lead {
			return withdrawThrough(leadWithAGrant(), ConsentConversationRequest, operatorActor(), message("m-1"))
		}, "ev-out", ErrOptOutNotRestorable},
		{"an opt-out recorded through Elo", func() *Lead { return withdrawThrough(leadWithAGrant(), ConsentElo, operatorActor(), note("pediu")) }, "ev-out", ErrOptOutNotRestorable},
		{"an opt-out from before the records", func() *Lead {
			at := consentAt
			return &Lead{ID: "l-1", OptedOutAt: &at, OptOutSource: OptOutLeadRequest}
		}, "ev-out", ErrOptOutNotRestorable},
		{"another withdrawal than the current one", func() *Lead { return withdrawThrough(leadWithAGrant(), ConsentKeyword, leadActor(), message("m-1")) }, "ev-older", ErrOptOutNotRestorable},
		{"a lead that is not opted out", leadWithAGrant, "ev-out", ErrLeadNotOptedOut},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := tc.lead()
			before := *l
			changed, err := l.ApplyConsentChange(revertOf(tc.id))
			if changed || !errors.Is(err, tc.want) {
				t.Fatalf("revert = %v, %v; want %v", changed, err, tc.want)
			}
			if l.ConsentRecord != before.ConsentRecord || l.OptedOutAt != before.OptedOutAt {
				t.Fatal("a refused revert changed the lead")
			}
		})
	}
}

func TestARevertNeedsTheUserAndTheirNote(t *testing.T) {
	long := make([]rune, MaxConsentExcerptLength+1)
	for i := range long {
		long[i] = 'a'
	}
	cases := []struct {
		name   string
		change ConsentChange
		want   error
	}{
		{"without a note", RevertWithdrawal("u-9", "   ", "ev-out", revertAt), ErrConsentEvidenceRequired},
		{"with a note longer than an excerpt", RevertWithdrawal("u-9", string(long), "ev-out", revertAt), ErrConsentExcerptTooLong},
		{"without the person", RevertWithdrawal("", "nota", "ev-out", revertAt), ErrConsentActorInvalid},
		{"without the withdrawal", RevertWithdrawal("u-9", "nota", " ", revertAt), ErrOptOutNotRestorable},
		{"without the time", RevertWithdrawal("u-9", "nota", "ev-out", time.Time{}), ErrConsentTimeRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ConsentRules(tc.change); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
	withSource := RevertWithdrawal("u-9", "nota", "ev-out", revertAt)
	withSource.Source = ConsentManual
	if err := ConsentRules(withSource); !errors.Is(err, ErrConsentNotAllowed) {
		t.Fatalf("a revert names no source of its own: %v", err)
	}
}

func TestTheWithdrawalToRevertIsTheCurrentAutomatedOneChosenBeforeTheRequest(t *testing.T) {
	l := withdrawThrough(leadWithAGrant(), ConsentButtonReply, leadActor(), tap("wamid.in"))
	if id, err := l.WithdrawalToRevert(consentAt.Add(2 * time.Hour)); err != nil || id != "ev-out" {
		t.Fatalf("got %q, %v", id, err)
	}
	if _, err := l.WithdrawalToRevert(consentAt.Add(30 * time.Minute)); !errors.Is(err, ErrOptOutNotRestorable) {
		t.Fatalf("a withdrawal newer than the request: %v", err)
	}
	if _, err := leadWithAGrant().WithdrawalToRevert(revertAt); !errors.Is(err, ErrLeadNotOptedOut) {
		t.Fatalf("not opted out: %v", err)
	}
	operator := withdrawThrough(leadWithAGrant(), ConsentManual, operatorActor(), ConsentEvidence{})
	if _, err := operator.WithdrawalToRevert(revertAt); !errors.Is(err, ErrOptOutNotRestorable) {
		t.Fatalf("operator opt-out: %v", err)
	}
}

func TestTheRevertEventNamesTheRevertedWithdrawalAndTheNote(t *testing.T) {
	before := withdrawThrough(leadWithAGrant(), ConsentKeyword, leadActor(), message("m-1"))
	after := *before
	c := revertOf("ev-out")
	if _, err := after.ApplyConsentChange(c); err != nil {
		t.Fatal(err)
	}
	event := ConsentEvent(c, before, &after)
	if event.Kind != EventConsentWithdrawalReverted || event.ID != "ev-revert" || event.Actor != "u-9" {
		t.Fatalf("event = %+v", event)
	}
	var detail map[string]any
	for _, ch := range event.Changes {
		if ch.Field == FieldConsent {
			detail, _ = ch.After.(map[string]any)
		}
	}
	if detail["kind"] != string(ConsentRevertWithdrawal) || detail["source"] != string(ConsentKeyword) || detail["revertedEventId"] != "ev-out" ||
		detail["note"] != "o botão SAIR estava ligado à opção errada" || detail["actorType"] != "user" || detail["actorId"] != "u-9" {
		t.Fatalf("detail = %+v", detail)
	}
}

func TestTheRevertCodes(t *testing.T) {
	if ErrorCode(ErrOptOutNotRestorable) != "lead_opt_out_not_restorable" || ErrorCode(ErrLeadNotOptedOut) != "lead_not_opted_out" {
		t.Fatal("codes")
	}
}
