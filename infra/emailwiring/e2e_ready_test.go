package emailwiring_test

import (
	"testing"

	"vozko/domain/emailcampaign"
	"vozko/domain/selection"
)

func TestEmailE2E_AReadyCampaignStartsLaterAndSendsAndBillsLikeADirectLaunch(t *testing.T) {
	w := newWorld(t)
	ws := w.sendingTenant("Loja Pronta", roomyWallet)
	unit := w.unit(ws)
	w.seedLeads(ws, 1200, "pronta")
	everyone := selection.Selection{Mode: selection.ModeEveryone}
	draft, err := w.campaigns.Service.Create(w.ctx, ws.Actor, emailcampaign.Draft{
		Name: "Pronta", Subject: "Oferta para {{first_name|você}}", Document: readyDocument(), Selection: &everyone,
	})
	w.must(err)

	ready, err := w.campaigns.Service.Ready(w.ctx, ws.Actor, draft.ID)
	w.must(err)
	if ready.Status != emailcampaign.StatusReady || w.wallet(ws) != ws.Start || len(w.batchDebits(ws, *ready)) != 0 || w.counts(*ready).Pending != 0 {
		t.Fatalf("ready = %s, wallet %d: nothing moves at ready", ready.Status, w.wallet(ws))
	}

	launched, err := w.campaigns.Service.Launch(w.ctx, ws.Actor, draft.ID, nil)
	w.must(err)
	if launched.Status != emailcampaign.StatusRunning || w.wallet(ws) != ws.Start {
		t.Fatalf("started = %s, wallet %d: nothing is charged at launch", launched.Status, w.wallet(ws))
	}
	w.dispatch(*launched, campaignWorkers, 0)

	final := w.campaign(*launched)
	if final.Status != emailcampaign.StatusCompleted {
		t.Fatalf("campaign = %s", final.Status)
	}
	if counts := w.counts(*launched); counts.Sent != 1200 || counts.Pending != 0 || counts.Failed != 0 {
		t.Fatalf("counts = %+v", counts)
	}
	w.assertBatches(ws, *launched, 1000, 200)
	if got, want := w.wallet(ws), ws.Start-1200*unit; got != want {
		t.Fatalf("wallet = %d, want %d", got, want)
	}
	w.assertDistinctSends(1200)
	w.assertSlots(*launched)
	w.assertLedger(ws)
}
