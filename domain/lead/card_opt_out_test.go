package lead

import (
	"testing"
	"time"
)

func TestTheCardShowsHowAnOptedOutLeadLeftAndTheProof(t *testing.T) {
	l := grantedLead()
	withdrawal := change(ConsentWithdraw, ConsentButtonReply, leadActor(), ConsentEvidence{Type: EvidenceButtonReply, Ref: "wamid.tap", Excerpt: "SAIR"})
	if _, err := l.ApplyConsentChange(withdrawal); err != nil {
		t.Fatal(err)
	}

	card := CardOf(l, Viewer{})

	record := card.OptOutRecord
	if record == nil {
		t.Fatal("an opted-out lead's card must carry how it left")
	}
	if record.Source != ConsentButtonReply || record.Actor != leadActor() || record.Evidence.Type != EvidenceButtonReply || record.Evidence.Excerpt != "SAIR" || !record.At.Equal(consentAt) {
		t.Fatalf("the card shows another record: %+v", record)
	}
	record.Evidence.Excerpt = "changed"
	if l.ConsentRecord.Evidence.Excerpt != "SAIR" {
		t.Fatal("the card must not share the lead's record")
	}
}

func TestTheCardCarriesNoOptOutRecordWhenTheLeadReceives(t *testing.T) {
	if card := CardOf(grantedLead(), Viewer{}); card.OptOutRecord != nil {
		t.Fatalf("a lead that receives has no opt-out record, got %+v", card.OptOutRecord)
	}

	regranted := optedOutLead()
	grant := change(ConsentGrant, ConsentManual, operatorActor(), note("pediu de novo por telefone"))
	grant.At = consentAt.Add(time.Hour)
	if _, err := regranted.ApplyConsentChange(grant); err != nil {
		t.Fatal(err)
	}
	if card := CardOf(regranted, Viewer{}); card.OptOutRecord != nil {
		t.Fatalf("a new grant replaces the opt-out, got %+v", card.OptOutRecord)
	}

	at := consentAt
	legacy := &Lead{ID: "l-3", OptedOutAt: &at, OptOutSource: OptOutOperator}
	if card := CardOf(legacy, Viewer{}); card.OptOutRecord != nil || card.OptedOutAt == nil {
		t.Fatalf("an opt-out from before the record keeps only its date and source, got %+v", card)
	}
}
