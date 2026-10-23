package campaignaudience_repository

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/campaign"
	audience "vozko/domain/campaignaudience"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

const productionPool = 10

var ctx = context.Background()

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := repotest.IsolatedDB(t, "campaign_audience", schema.CampaignAudienceModels()...)
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	pool.SetMaxOpenConns(productionPool)
	return db
}

func newList(workspaceID, actorID string, now time.Time, rows ...campaign.ListRow) (audience.List, []audience.Row) {
	l := audience.NewList(uuid.NewString(), workspaceID, actorID, audience.ChannelWhatsApp, "lista.csv", campaign.ContactList{
		Layout: campaign.ListLayout{Header: true, Headers: []string{"telefone", "nome", "var1"}, Width: 3, Contact: 0, Name: 1}, Lines: len(rows) + 1,
		IssueCounts: map[campaign.ListIssueReason]int{campaign.ListInvalidNumber: 1},
		Issues:      []campaign.ListIssue{{Line: 9, Reason: campaign.ListInvalidNumber, Raw: "1;x"}},
	}, now)
	out := make([]audience.Row, 0, len(rows))
	for _, r := range rows {
		out = append(out, audience.Row{ID: uuid.NewString(), ListID: l.ID, ListRow: r})
	}
	return l, out
}

func TestListsRoundTripAgainstPostgres(t *testing.T) {
	db := testDB(t)
	repo := New(db)
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	ws, actor := uuid.NewString(), uuid.NewString()
	l, rows := newList(ws, actor, now,
		campaign.ListRow{Line: 2, Contact: "5511900010142", Name: "Maria", Cells: []string{"5511900010142", "Maria", "M"}},
		campaign.ListRow{Line: 3, Contact: "5511900023301", Name: "João", Cells: []string{"5511900023301", "João", ""}},
	)
	if err := repo.Create(ctx, l, rows); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, ws, l.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Layout.Headers[2] != "var1" || got.IssueCounts[campaign.ListInvalidNumber] != 1 || got.Issues[0].Raw != "1;x" || got.ExpiresAt == nil || !got.ExpiresAt.Equal(now.Add(audience.ListTTL)) || got.CreatedBy != actor || got.Channel != audience.ChannelWhatsApp {
		t.Fatalf("list = %+v", got)
	}
	if _, err := repo.Get(ctx, uuid.NewString(), l.ID); !errors.Is(err, audience.ErrListNotFound) {
		t.Fatalf("another workspace = %v", err)
	}
	if _, err := repo.Get(ctx, ws, "not-a-uuid"); !errors.Is(err, audience.ErrListNotFound) {
		t.Fatalf("a malformed id = %v", err)
	}

	first, err := repo.RowsAfter(ctx, l.ID, audience.RowCursor{Line: -1}, 1)
	if err != nil || len(first) != 1 || first[0].Name != "Maria" || !reflect.DeepEqual(first[0].Cells, []string{"5511900010142", "Maria", "M"}) || first[0].Variables != nil || first[0].Metadata != nil {
		t.Fatalf("first page = %+v, %v", first, err)
	}
	second, err := repo.RowsAfter(ctx, l.ID, audience.RowCursor{Line: first[0].Line, ID: first[0].ID}, 5)
	if err != nil || len(second) != 1 || second[0].Name != "João" {
		t.Fatalf("second page = %+v, %v", second, err)
	}

	manual := audience.Row{ID: uuid.NewString(), ListID: l.ID, ListRow: campaign.ListRow{Contact: "5511900033301", Name: "Rita 100%", Variables: []string{"R"}, Metadata: map[string]any{"serie": "2"}}}
	if err := repo.AddRow(ctx, manual); err != nil {
		t.Fatal(err)
	}
	dup := audience.Row{ID: uuid.NewString(), ListID: l.ID, ListRow: campaign.ListRow{Contact: "5511900033301"}}
	if err := repo.AddRow(ctx, dup); !errors.Is(err, audience.ErrRowDuplicate) {
		t.Fatalf("duplicate = %v", err)
	}
	found, total, err := repo.SearchRows(ctx, l.ID, audience.RowQuery{Search: "100%", Limit: 10})
	if err != nil || total != 1 || found[0].ID != manual.ID || !reflect.DeepEqual(found[0].Variables, []string{"R"}) || found[0].Metadata["serie"] != "2" {
		t.Fatalf("search = %+v %d %v", found, total, err)
	}
	page, total, err := repo.SearchRows(ctx, l.ID, audience.RowQuery{Offset: 1, Limit: 1})
	if err != nil || total != 3 || len(page) != 1 || page[0].Name != "Maria" {
		t.Fatalf("manual rows (line 0) come first: %+v %d %v", page, total, err)
	}
	edited := manual
	edited.Contact = "5511900010142"
	if err := repo.UpdateRow(ctx, edited); !errors.Is(err, audience.ErrRowDuplicate) {
		t.Fatalf("an edit onto another contact = %v", err)
	}
	edited.Contact, edited.Name = "5511900044401", "Rita Lima"
	if err := repo.UpdateRow(ctx, edited); err != nil {
		t.Fatal(err)
	}
	stored, err := repo.Row(ctx, l.ID, manual.ID)
	if err != nil || stored.Contact != "5511900044401" || stored.Name != "Rita Lima" {
		t.Fatalf("stored = %+v, %v", stored, err)
	}
	if _, err := repo.Row(ctx, l.ID, "bad"); !errors.Is(err, audience.ErrRowNotFound) {
		t.Fatalf("malformed row id = %v", err)
	}
	if err := repo.UpdateRow(ctx, audience.Row{ID: uuid.NewString(), ListID: l.ID, ListRow: campaign.ListRow{Contact: "5511900099901"}}); !errors.Is(err, audience.ErrRowNotFound) {
		t.Fatalf("missing row = %v", err)
	}
	if err := repo.DeleteRow(ctx, l.ID, manual.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteRow(ctx, l.ID, manual.ID); !errors.Is(err, audience.ErrRowNotFound) {
		t.Fatalf("second delete = %v", err)
	}
	if n, err := repo.CountRows(ctx, l.ID); err != nil || n != 2 {
		t.Fatalf("count = %d, %v", n, err)
	}

	l.AttachedTo = audience.EmailCampaignOwner(uuid.NewString())
	l.Touch(now.Add(time.Hour))
	if err := repo.Save(ctx, l); err != nil {
		t.Fatal(err)
	}
	saved, err := repo.Get(ctx, ws, l.ID)
	if err != nil || saved.AttachedTo != l.AttachedTo || saved.ExpiresAt != nil {
		t.Fatalf("saved = %+v, %v", saved, err)
	}
	if err := repo.Delete(ctx, ws, l.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get(ctx, ws, l.ID); !errors.Is(err, audience.ErrListNotFound) {
		t.Fatalf("deleted list = %v", err)
	}
	var orphans int64
	db.Model(&schema.CampaignAudienceListRow{}).Where("list_id = ?", l.ID).Count(&orphans)
	if orphans != 0 {
		t.Fatalf("%d rows outlived their list", orphans)
	}
}

func TestDeleteExpiredLeavesAttachedAndFreshLists(t *testing.T) {
	db := testDB(t)
	repo := New(db)
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	ws, actor := uuid.NewString(), uuid.NewString()
	old, oldRows := newList(ws, actor, now.Add(-48*time.Hour), campaign.ListRow{Contact: "5511900010142"})
	fresh, _ := newList(ws, actor, now)
	attached, _ := newList(ws, actor, now.Add(-48*time.Hour))
	attached.AttachedTo = "email_campaign:x"
	attached.Touch(now.Add(-48 * time.Hour))
	for _, l := range []audience.List{old, fresh, attached} {
		rows := []audience.Row(nil)
		if l.ID == old.ID {
			rows = oldRows
		}
		if err := repo.Create(ctx, l, rows); err != nil {
			t.Fatal(err)
		}
	}
	n, err := repo.DeleteExpired(ctx, now, 10)
	if err != nil || n != 1 {
		t.Fatalf("DeleteExpired = %d, %v", n, err)
	}
	for id, want := range map[string]bool{old.ID: false, fresh.ID: true, attached.ID: true} {
		_, err := repo.Get(ctx, ws, id)
		if (err == nil) != want {
			t.Fatalf("list %s present = %v, want %v", id, err == nil, want)
		}
	}
}

func TestCreateWritesALargeListInOneTransaction(t *testing.T) {
	db := testDB(t)
	repo := New(db)
	now := time.Now().UTC()
	rows := make([]campaign.ListRow, 0, 2500)
	for i := 0; i < 2500; i++ {
		rows = append(rows, campaign.ListRow{Line: i + 2, Contact: fmt.Sprintf("55119%08d", i)})
	}
	l, stored := newList(uuid.NewString(), uuid.NewString(), now, rows...)
	stored = append(stored, audience.Row{ID: uuid.NewString(), ListID: l.ID, ListRow: campaign.ListRow{Contact: "5511900000000"}})
	if err := repo.Create(ctx, l, stored); err == nil {
		t.Fatal("a duplicated contact was accepted")
	}
	var lists int64
	db.Model(&schema.CampaignAudienceList{}).Where("id = ?", l.ID).Count(&lists)
	if lists != 0 {
		t.Fatal("a failed create left the list behind")
	}
	if err := repo.Create(ctx, l, stored[:2500]); err != nil {
		t.Fatal(err)
	}
	if n, _ := repo.CountRows(ctx, l.ID); n != 2500 {
		t.Fatalf("rows = %d", n)
	}
}

func TestSuggestionsReplaceTheWholeSetPerWorkspaceAndTemplate(t *testing.T) {
	db := testDB(t)
	repo := NewSuggestions(db)
	ws, other, tpl, actor := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	if err := repo.Replace(ctx, ws, tpl, actor, []audience.Suggestion{{Slot: 2, Source: campaign.BindCity}, {Slot: 1, Source: campaign.BindFirstName}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Replace(ctx, other, tpl, actor, []audience.Suggestion{{Slot: 1, Source: campaign.BindName}}); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, ws, tpl)
	if err != nil || !reflect.DeepEqual(got, []audience.Suggestion{{Slot: 1, Source: campaign.BindFirstName}, {Slot: 2, Source: campaign.BindCity}}) {
		t.Fatalf("suggestions = %+v, %v", got, err)
	}
	if err := repo.Replace(ctx, ws, tpl, actor, nil); err != nil {
		t.Fatal(err)
	}
	if got, err := repo.Get(ctx, ws, tpl); err != nil || len(got) != 0 {
		t.Fatalf("cleared = %+v, %v", got, err)
	}
	if got, err := repo.Get(ctx, other, tpl); err != nil || len(got) != 1 {
		t.Fatalf("another workspace kept its own: %+v, %v", got, err)
	}
	if got, err := repo.Get(ctx, ws, "not-a-uuid"); err != nil || len(got) != 0 {
		t.Fatalf("a malformed template = %+v, %v", got, err)
	}
}
