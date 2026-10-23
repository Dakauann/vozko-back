package emailwiring_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	audience "vozko/domain/campaignaudience"
	"vozko/domain/emailcampaign"
	"vozko/domain/emaildesign"
	"vozko/domain/selection"
	"vozko/infra/database/schema"
	campaignaudience_usecase "vozko/usecases/campaignaudience"
)

func (w *world) leadsWithEmail(ws *tenant, address string) int64 {
	w.t.Helper()
	var n int64
	w.must(w.db.Raw("SELECT count(*) FROM leads WHERE workspace_id = ? AND lower(email) = ? AND deleted_at IS NULL", ws.ID, address).Scan(&n).Error)
	return n
}

func TestEmailE2E_AnUploadedListBecomesLeadsAtLaunchAndIsSent(t *testing.T) {
	w := newWorld(t)
	ws := w.sendingTenant("Escola Lista", roomyWallet)
	known := schema.Lead{ID: uuid.NewString(), WorkspaceID: ws.ID, Name: "Ana Antiga", Email: schema.OptionalText("ana@escola.example")}
	w.must(w.db.Create(&known).Error)

	preview, err := w.audience.Upload(w.ctx, ws.Actor, audience.ChannelEmail, campaignaudience_usecase.Upload{FileName: "pais.csv",
		Data: []byte("Nome;E-mail\nAna;ANA@escola.example\nBia;bia@escola.example\nSem email;\nBia de novo;bia@escola.example\n")}, campaignaudience_usecase.View{})
	w.must(err)
	if preview.Rows != 2 || preview.Existing != 1 || preview.New != 1 || preview.Skipped.Total != 2 {
		t.Fatalf("preview = %+v", preview)
	}
	if w.leadsWithEmail(ws, "bia@escola.example") != 0 {
		t.Fatal("the preview created a lead")
	}
	count, err := w.audience.Count(w.ctx, ws.Actor, audience.ChannelEmail, campaignaudience_usecase.Params{Subject: "Oi {{first_name|família}}", Document: docPtr()}, audience.Spec{Source: audience.SourceList, ListID: preview.ID})
	w.must(err)
	if count.Matched != 2 || count.Sendable == nil || *count.Sendable != 2 || count.List.New != 1 {
		t.Fatalf("draft-free count = %+v", count)
	}

	draft, err := w.campaigns.Service.Create(w.ctx, ws.Actor, emailcampaign.Draft{Name: "Matrículas", Subject: "Oi {{first_name|família}}", Document: readyDocument(), ListID: preview.ID})
	w.must(err)
	review, err := w.campaigns.Service.Review(w.ctx, ws.Actor, draft.ID)
	w.must(err)
	if review.Audience.Sendable != 2 || review.Audience.List == nil || review.Audience.List.Existing != 1 {
		t.Fatalf("review = %+v", review.Audience)
	}
	launched, err := w.campaigns.Service.Launch(w.ctx, ws.Actor, draft.ID, nil)
	w.must(err)
	if w.leadsWithEmail(ws, "bia@escola.example") != 1 || w.leadsWithEmail(ws, "ana@escola.example") != 1 {
		t.Fatal("launch did not match Ana and create Bia exactly once")
	}
	var source string
	w.must(w.db.Raw("SELECT source FROM leads WHERE workspace_id = ? AND lower(email) = 'bia@escola.example'", ws.ID).Scan(&source).Error)
	if source != "import" {
		t.Fatalf("Bia came from %q, want import", source)
	}
	w.dispatch(*launched, campaignWorkers, 0)
	if counts := w.counts(*launched); counts.Sent != 2 || counts.SkippedTotal() != 0 {
		t.Fatalf("counts = %+v", counts)
	}
	var stored int64
	w.must(w.db.Raw("SELECT count(*) FROM campaign_audience_lists WHERE id = ?", preview.ID).Scan(&stored).Error)
	if stored != 0 {
		t.Fatal("the list outlived the launch")
	}
}

func TestEmailE2E_AnExcludedMergeTagLeavesOutLeadsWithoutTheValue(t *testing.T) {
	w := newWorld(t)
	ws := w.sendingTenant("Escola Série", roomyWallet)
	withSerie := schema.Lead{ID: uuid.NewString(), WorkspaceID: ws.ID, Name: "Ana", Email: schema.OptionalText("ana@serie.example")}
	withSerie.CustomFields = []byte(`{"serie":"1º ano"}`)
	without := schema.Lead{ID: uuid.NewString(), WorkspaceID: ws.ID, Name: "Bia", Email: schema.OptionalText("bia@serie.example")}
	w.must(w.db.Create(&withSerie).Error)
	w.must(w.db.Create(&without).Error)
	everyone := selection.Selection{Mode: selection.ModeEveryone}
	doc := readyDocument()
	doc.Blocks[0].Heading.Text = "Série {{field.serie}}"
	draft, err := w.campaigns.Service.Create(w.ctx, ws.Actor, emailcampaign.Draft{Name: "Série", Subject: "Oi {{first_name|família}}", Document: doc, Selection: &everyone,
		MergePolicies: []audience.Variable{{Key: "field.serie", Missing: audience.MissingExclude}}})
	w.must(err)
	launched, err := w.campaigns.Service.Launch(w.ctx, ws.Actor, draft.ID, nil)
	w.must(err)
	counts := w.counts(*launched)
	if counts.Pending != 1 || counts.Skipped[emailcampaign.SkipMissingVariable] != 1 {
		t.Fatalf("counts = %+v", counts)
	}
	w.dispatch(*launched, campaignWorkers, 0)
	sent := w.ses.Sent()
	if len(sent) != 1 || sent[0].Message.To != "ana@serie.example" || !strings.Contains(sent[0].Message.HTML, "1º ano") {
		t.Fatalf("sends = %d", len(sent))
	}
}

func TestEmailE2E_AListOfAnotherMemberIsRefused(t *testing.T) {
	w := newWorld(t)
	ws := w.sendingTenant("Escola Membros", roomyWallet)
	preview, err := w.audience.Upload(w.ctx, ws.Actor, audience.ChannelEmail, campaignaudience_usecase.Upload{Data: []byte("email\nana@x.example\n")}, campaignaudience_usecase.View{})
	w.must(err)
	other := ws.Actor
	other.UserID = uuid.NewString()
	if _, err := w.campaigns.Service.Create(w.ctx, other, emailcampaign.Draft{Name: "X", ListID: preview.ID}); !errors.Is(err, audience.ErrListNotFound) {
		t.Fatalf("another member's list = %v", err)
	}
}

func docPtr() *emaildesign.Document {
	doc := readyDocument()
	return &doc
}
