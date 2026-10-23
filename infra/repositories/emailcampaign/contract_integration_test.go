package emailcampaign

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	domain "vozko/domain/emailcampaign"
	"vozko/infra/database/schema"
)

func TestSettlementIsStoredOnceAgainstPostgres(t *testing.T) {
	db := migrated(t)
	ctx := context.Background()
	repo := NewCampaignRepository(db)
	c := &domain.Campaign{WorkspaceID: uuid.NewString(), Name: "Settle", Status: domain.StatusRunning}
	if err := repo.Create(ctx, c); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 21, 18, 0, 0, 0, time.UTC)
	ok, err := repo.SetSettlement(ctx, c.ID, domain.RunSettlement{RefundedUnits: 4, RefundedMicros: 2400, SettledAt: at})
	if err != nil || !ok {
		t.Fatalf("first settlement = %v %v", ok, err)
	}
	ok, err = repo.SetSettlement(ctx, c.ID, domain.RunSettlement{RefundedUnits: 9, RefundedMicros: 5400, SettledAt: at.Add(time.Hour)})
	if err != nil || ok {
		t.Fatalf("a settled campaign is never settled again: %v %v", ok, err)
	}
	got, err := repo.Get(ctx, c.WorkspaceID, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Settlement == nil || got.Settlement.RefundedUnits != 4 || got.Settlement.RefundedMicros != 2400 || !got.Settlement.SettledAt.Equal(at) {
		t.Fatalf("settlement = %+v", got.Settlement)
	}
	fresh := &domain.Campaign{WorkspaceID: c.WorkspaceID, Name: "Fresh", Status: domain.StatusDraft}
	if err := repo.Create(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	if got, _ := repo.Get(ctx, fresh.WorkspaceID, fresh.ID); got.Settlement != nil {
		t.Fatalf("an unsettled campaign has no settlement: %+v", got.Settlement)
	}
}

func TestSummariesAggregateThePageInOneQueryAgainstPostgres(t *testing.T) {
	db := migrated(t)
	ctx := context.Background()
	ws := uuid.NewString()
	first, second, silent := uuid.NewString(), uuid.NewString(), uuid.NewString()
	now := time.Now().UTC().Truncate(time.Second)
	message := func(runID, runKind string, delivered, opened, clicked bool) schema.EmailMessage {
		m := schema.EmailMessage{ID: uuid.NewString(), WorkspaceID: ws, RunKind: runKind, RunID: runID, DomainID: uuid.NewString(), Address: "a@b.com", Status: "sent", SentAt: &now, CreatedAt: now, UpdatedAt: now}
		if delivered {
			m.DeliveredAt = &now
		}
		if opened {
			m.FirstOpenAt = &now
		}
		if clicked {
			m.FirstClickAt = &now
		}
		return m
	}
	for _, m := range []schema.EmailMessage{
		message(first, "campaign", true, true, true),
		message(first, "campaign", true, false, false),
		message(first, "campaign", false, false, false),
		message(first, "test", true, true, true),
		message(second, "campaign", true, true, false),
	} {
		if err := db.Create(&m).Error; err != nil {
			t.Fatal(err)
		}
	}
	metrics := NewMetricsRepository(db)
	got, err := metrics.Summaries(ctx, ws, []string{first, second, silent, "not-a-uuid"})
	if err != nil {
		t.Fatal(err)
	}
	if f := got[first]; f.Sent != 3 || f.Delivered != 2 || f.Opens != 1 || f.Clicks != 1 {
		t.Fatalf("first = %+v", f)
	}
	if s := got[second]; s.Sent != 1 || s.Delivered != 1 || s.Opens != 1 {
		t.Fatalf("second = %+v", s)
	}
	if _, ok := got[silent]; ok {
		t.Fatal("a campaign without messages has no row")
	}
	if other, _ := metrics.Summaries(ctx, uuid.NewString(), []string{first}); len(other) != 0 {
		t.Fatal("summaries are scoped to the workspace")
	}
	if empty, err := metrics.Summaries(ctx, ws, nil); err != nil || len(empty) != 0 {
		t.Fatalf("no ids = %v %v", empty, err)
	}
}

func TestDailyBucketsAreBrasiliaDaysAgainstPostgres(t *testing.T) {
	db := migrated(t)
	ctx := context.Background()
	ws, campaignID := uuid.NewString(), uuid.NewString()
	lateEvening := time.Date(2026, 10, 21, 2, 30, 0, 0, time.UTC)
	morning := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	for _, at := range []time.Time{lateEvening, morning} {
		sent := at
		m := schema.EmailMessage{ID: uuid.NewString(), WorkspaceID: ws, RunKind: "campaign", RunID: campaignID, DomainID: uuid.NewString(), Address: "a@b.com", Status: "sent", SentAt: &sent, CreatedAt: at, UpdatedAt: at}
		if err := db.Create(&m).Error; err != nil {
			t.Fatal(err)
		}
	}
	metrics := NewMetricsRepository(db)
	overview, err := metrics.Overview(ctx, ws, time.Date(2026, 10, 19, 3, 0, 0, 0, time.UTC), 5)
	if err != nil {
		t.Fatal(err)
	}
	wantDays := []time.Time{time.Date(2026, 10, 20, 3, 0, 0, 0, time.UTC), time.Date(2026, 10, 21, 3, 0, 0, 0, time.UTC)}
	if len(overview.Daily) != 2 || !overview.Daily[0].Bucket.Equal(wantDays[0]) || !overview.Daily[1].Bucket.Equal(wantDays[1]) {
		t.Fatalf("overview daily = %+v", overview.Daily)
	}
	timeline, err := metrics.Timeline(ctx, ws, campaignID, domain.BucketDay)
	if err != nil || len(timeline) != 2 || !timeline[0].Bucket.Equal(wantDays[0]) {
		t.Fatalf("timeline = %+v err = %v", timeline, err)
	}
}

func TestRecipientsCarryTheLeadNameAgainstPostgres(t *testing.T) {
	db := migrated(t)
	ctx := context.Background()
	ws, otherWS, campaignID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	ana, foreign, gone := uuid.NewString(), uuid.NewString(), uuid.NewString()
	if err := db.Exec("INSERT INTO leads (id, workspace_id, name, deleted_at) VALUES (?, ?, 'Ana Souza', NULL), (?, ?, 'Outra', NULL), (?, ?, 'Antiga', now())",
		ana, ws, foreign, otherWS, gone, ws).Error; err != nil {
		t.Fatal(err)
	}
	recipients := NewRecipientRepository(db)
	if err := recipients.Replace(ctx, ws, campaignID, []domain.Recipient{
		{LeadID: ana, Address: "ana@x.com", Status: domain.RecipientPending, Variant: domain.VariantA},
		{LeadID: foreign, Address: "outra@x.com", Status: domain.RecipientPending, Variant: domain.VariantA},
		{LeadID: gone, Address: "antiga@x.com", Status: domain.RecipientPending, Variant: domain.VariantA},
		{Address: "solta@x.com", Status: domain.RecipientPending, Variant: domain.VariantA},
	}); err != nil {
		t.Fatal(err)
	}
	list, _, err := recipients.List(ctx, ws, campaignID, domain.RecipientFilter{}, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, r := range list {
		names[string(r.Address)] = r.LeadName
	}
	if names["ana@x.com"] != "Ana Souza" || names["outra@x.com"] != "" || names["antiga@x.com"] != "" || names["solta@x.com"] != "" || len(names) != 4 {
		t.Fatalf("names = %v", names)
	}
}
