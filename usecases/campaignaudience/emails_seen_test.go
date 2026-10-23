package campaignaudience_usecase

import (
	"slices"
	"testing"

	"vozko/domain/campaignaudience"
	"vozko/domain/emailaddress"
)

func TestAnUploadedEmailListStartsTheDomainChecksOfItsAddresses(t *testing.T) {
	w := newWorld(t)
	preview := w.upload(t, campaignaudience.ChannelEmail, "email;nome\nana@exemplo.com.br;Ana\nbia@nova.com.br;Bia\n", View{})
	if !slices.Equal(w.seen.Seen(), []emailaddress.Address{"ana@exemplo.com.br", "bia@nova.com.br"}) {
		t.Fatalf("seen %v, want every uploaded address", w.seen.Seen())
	}
	if _, err := w.service.AddRow(ctx, testActor, campaignaudience.ChannelEmail, preview.ID, RowInput{Contact: "caio@outra.com.br"}); err != nil {
		t.Fatal(err)
	}
	row, err := w.service.AddRow(ctx, testActor, campaignaudience.ChannelEmail, preview.ID, RowInput{Contact: "duda@outra.com.br"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.service.EditRow(ctx, testActor, campaignaudience.ChannelEmail, preview.ID, row.ID, RowInput{Contact: "duda@quarta.com.br"}); err != nil {
		t.Fatal(err)
	}
	seen := w.seen.Seen()
	if !slices.Equal(seen[2:], []emailaddress.Address{"caio@outra.com.br", "duda@outra.com.br", "duda@quarta.com.br"}) {
		t.Fatalf("seen %v, want added and edited rows too", seen)
	}
}

func TestAPhoneListSeesNoEmail(t *testing.T) {
	w := newWorld(t)
	w.upload(t, campaignaudience.ChannelWhatsApp, "telefone;nome\n5511900010142;Maria\n", View{})
	if len(w.seen.Seen()) != 0 {
		t.Fatalf("seen %v", w.seen.Seen())
	}
}
