package emailwiring_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	audience "vozko/domain/campaignaudience"
	"vozko/domain/emailcampaign"
	"vozko/domain/selection"
	"vozko/infra/database/schema"
	lead_repository "vozko/infra/repositories/lead"
	campaignaudience_usecase "vozko/usecases/campaignaudience"
)

func TestEmailE2E_TheReviewJudgesLeadsFreshAfterALeadEdit(t *testing.T) {
	w := newWorld(t)
	ws := w.sendingTenant("Escola Revisão", roomyWallet)
	const crowd = 3000
	w.seedLeads(ws, crowd, "massa")
	bia := schema.Lead{ID: uuid.NewString(), WorkspaceID: ws.ID, Name: "Bia", Version: 1}
	w.must(w.db.Create(&bia).Error)
	everyone := selection.Selection{Mode: selection.ModeEveryone}
	draft, err := w.campaigns.Service.Create(w.ctx, ws.Actor, emailcampaign.Draft{Name: "Revisão", Subject: "Oi {{first_name|família}}", Document: readyDocument(), Selection: &everyone})
	w.must(err)

	started := time.Now()
	before, err := w.campaigns.Service.Review(w.ctx, ws.Actor, draft.ID)
	w.must(err)
	reviewTook := time.Since(started)
	if before.Audience.Skipped[emailcampaign.SkipNoEmail] != 1 || before.Audience.Sendable != crowd {
		t.Fatalf("review before the edit = %+v", before.Audience)
	}
	doc := readyDocument()
	started = time.Now()
	_, err = w.audience.Count(w.ctx, ws.Actor, audience.ChannelEmail, campaignaudience_usecase.Params{Subject: "Oi {{first_name|família}}", Document: &doc}, audience.Spec{Source: audience.SourceLeads, Selection: &everyone})
	w.must(err)
	t.Logf("review of %d leads took %s, the count %s", crowd+1, reviewTook, time.Since(started))

	leads := lead_repository.NewRepository(w.db)
	current, err := leads.Load(w.ctx, ws.ID, bia.ID)
	w.must(err)
	current.Email = "bia@revisao.example"
	w.must(leads.Save(w.ctx, current, current.Version, nil))

	after, err := w.campaigns.Service.Review(w.ctx, ws.Actor, draft.ID)
	w.must(err)
	if after.Audience.Skipped[emailcampaign.SkipNoEmail] != 0 || after.Audience.Sendable != crowd+1 {
		t.Fatalf("review after the edit = %+v", after.Audience)
	}
}
