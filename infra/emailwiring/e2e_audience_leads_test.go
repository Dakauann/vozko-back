package emailwiring_test

import (
	"testing"

	"github.com/google/uuid"

	audience "vozko/domain/campaignaudience"
	"vozko/domain/emailcampaign"
	"vozko/domain/selection"
	"vozko/infra/database/schema"
	lead_repository "vozko/infra/repositories/lead"
	campaignaudience_usecase "vozko/usecases/campaignaudience"
	"vozko/usecases/campaignaudience/audiencecheck"
)

func TestEmailE2E_TheLeadWithoutAnEmailIsListedAndReadyOnceTheEmailIsAdded(t *testing.T) {
	w := newWorld(t)
	ws := w.sendingTenant("Escola Público", roomyWallet)
	ana := schema.Lead{ID: uuid.NewString(), WorkspaceID: ws.ID, Name: "Ana", Email: schema.OptionalText("ana@publico.example"), Version: 1}
	ana.CustomFields = []byte(`{"serie":"1º ano"}`)
	bia := schema.Lead{ID: uuid.NewString(), WorkspaceID: ws.ID, Name: "Bia", Version: 1}
	bia.CustomFields = []byte(`{"serie":"2º ano"}`)
	caio := schema.Lead{ID: uuid.NewString(), WorkspaceID: ws.ID, Name: "Caio", Email: schema.OptionalText("caio@publico.example"), Version: 1}
	for _, l := range []*schema.Lead{&ana, &bia, &caio} {
		w.must(w.db.Create(l).Error)
	}
	doc := readyDocument()
	doc.Blocks[0].Heading.Text = "Série {{field.serie}}"
	check := audiencecheck.Audience{Service: w.audience, Actor: ws.Actor, Channel: audience.ChannelEmail,
		Params: campaignaudience_usecase.Params{Subject: "Oi {{first_name|família}}", Document: &doc},
		Spec:   audience.Spec{Source: audience.SourceLeads, Selection: &selection.Selection{Mode: selection.ModeEveryone}, Variables: []audience.Variable{{Key: "field.serie", Missing: audience.MissingExclude}}}}

	count := check.RequireLeadsMatchTheCount(t)
	if count.Out[string(emailcampaign.SkipNoEmail)] != 1 || count.Out[audience.ReasonMissingVariable] != 1 || *count.Sendable != 1 {
		t.Fatalf("count = %+v", count)
	}
	noEmail := check.Leads(t, audience.LeadFilter{Reason: string(emailcampaign.SkipNoEmail)})
	if noEmail.Total != 1 || noEmail.Items[0].LeadID != bia.ID || noEmail.Items[0].Name != "Bia" {
		t.Fatalf("the lead without an email = %+v", noEmail)
	}
	noSerie := check.Leads(t, audience.LeadFilter{Variable: "field.serie"})
	if noSerie.Total != 1 || noSerie.Items[0].LeadID != caio.ID || noSerie.Items[0].Contact != "caio@publico.example" {
		t.Fatalf("the lead without a série = %+v", noSerie)
	}

	leads := lead_repository.NewRepository(w.db)
	current, err := leads.Load(w.ctx, ws.ID, bia.ID)
	w.must(err)
	current.Email = "bia@publico.example"
	w.must(leads.Save(w.ctx, current, current.Version, nil))
	current, err = leads.Load(w.ctx, ws.ID, caio.ID)
	w.must(err)
	current.CustomFields = map[string]any{"serie": "3º ano"}
	w.must(leads.Save(w.ctx, current, current.Version, nil))

	fixed := check.RequireLeadsMatchTheCount(t)
	if fixed.OutTotal != 0 || *fixed.Sendable != 3 {
		t.Fatalf("after the edits = %+v", fixed)
	}
	ready := check.Leads(t, audience.LeadFilter{Status: audience.LeadReady, Search: "bia@"})
	if ready.Total != 1 || ready.Items[0].Variables[len(ready.Items[0].Variables)-1] != "2º ano" {
		t.Fatalf("Bia after the edit = %+v", ready)
	}
}
