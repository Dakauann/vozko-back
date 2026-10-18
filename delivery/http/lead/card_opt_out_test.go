package lead

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	leaddomain "vozko/domain/lead"
)

func TestTheInboxCardAnswersHowTheLeadLeftAndTheProof(t *testing.T) {
	at := time.Date(2026, 10, 16, 16, 59, 0, 0, time.UTC)
	confirmed := at.Add(time.Second)
	record := &leaddomain.ConsentRecord{
		Kind: leaddomain.ConsentWithdraw, Source: leaddomain.ConsentButtonReply,
		Actor:    leaddomain.ConsentActor{Type: leaddomain.ConsentActorLead, ID: "l-1"},
		Evidence: leaddomain.ConsentEvidence{Type: leaddomain.EvidenceButtonReply, Ref: "wamid.tap", Excerpt: "SAIR"},
		At:       at, EventID: "ev-1", ConfirmationSentAt: &confirmed,
	}
	history := &stubHistory{card: leaddomain.Card{LeadID: routeLeadID, OptedOutAt: &at, OptOutSource: leaddomain.OptOutLeadRequest, OptOutRecord: record}}

	rec := send(t, routedHandler(&stubCommands{}, history), http.MethodGet, "/entries/"+relativeLeadID+"/lead", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var out LeadCardResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	want := LeadCardOptOutResponse{
		At: "2026-10-16T16:59:00Z", Source: "button_reply", ActorType: "lead",
		EvidenceType: "button_reply", Excerpt: "SAIR", ConfirmationSentAt: "2026-10-16T16:59:01Z",
	}
	if out.OptOut == nil || *out.OptOut != want {
		t.Fatalf("optOut = %+v, want %+v", out.OptOut, want)
	}

	plain := send(t, routedHandler(&stubCommands{}, &stubHistory{card: leaddomain.Card{LeadID: routeLeadID}}), http.MethodGet, "/entries/"+relativeLeadID+"/lead", nil, nil)
	var raw map[string]any
	if err := json.Unmarshal(plain.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if _, present := raw["optOut"]; present {
		t.Fatal("optOut must be absent on a lead that receives")
	}
}
