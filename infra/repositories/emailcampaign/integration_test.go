package emailcampaign

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/crmfilter"
	"vozko/domain/emailaddress"
	domain "vozko/domain/emailcampaign"
	"vozko/domain/emailconsent"
	"vozko/domain/emaildesign"
	"vozko/domain/selection"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func migrated(t *testing.T) *gorm.DB {
	t.Helper()
	db := repotest.IsolatedDB(t, "email_campaigns")
	db.Config.DisableForeignKeyConstraintWhenMigrating = true
	if err := db.AutoMigrate(append(schema.EmailCampaignModels(), schema.EmailSendingModels()...)...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := db.Exec("CREATE TABLE leads (id uuid PRIMARY KEY, workspace_id uuid NOT NULL, name varchar(255), deleted_at timestamptz)").Error; err != nil {
		t.Fatalf("leads: %v", err)
	}
	return db
}

func TestCampaignRoundTripAgainstPostgres(t *testing.T) {
	db := migrated(t)
	ctx := context.Background()
	repo := NewCampaignRepository(db)
	filter := crmfilter.Filter{Groups: []crmfilter.Group{{Conjunction: crmfilter.And, Predicates: []crmfilter.Predicate{{Field: crmfilter.FieldBlocked, Operator: crmfilter.OpIsFalse}}}}}
	c := &domain.Campaign{
		WorkspaceID: uuid.NewString(), Name: "Black Friday", Status: domain.StatusDraft, Subject: "Oi",
		Document:  emaildesign.Document{Version: 1, Blocks: []emaildesign.Block{{ID: "h", Kind: emaildesign.KindHeading, Heading: &emaildesign.Heading{Text: "Oi", Level: 1}}}},
		Selection: &selection.Selection{Mode: selection.ModeAllMatching, Filter: &filter},
		ABTest:    &domain.ABTest{VariantBSubject: "B", TestPercent: 20, WinnerMetric: domain.WinnerOpens, DecideAfterMinutes: 30},
		CreatedBy: uuid.NewString(),
	}
	if err := repo.Create(ctx, c); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, c.WorkspaceID, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Selection == nil || got.Selection.Filter == nil || got.ABTest == nil || got.ABTest.TestPercent != 20 || len(got.Document.Blocks) != 1 {
		t.Fatalf("round trip = %+v", got)
	}
	started := time.Now().UTC().Add(-time.Hour)
	ok, err := repo.MarkLaunched(ctx, c.ID, domain.StatusDraft, domain.Launch{StartedAt: started, LaunchedBy: c.CreatedBy})
	if err != nil || !ok {
		t.Fatalf("MarkLaunched = %v %v", ok, err)
	}
	if ok, _ := repo.MarkLaunched(ctx, c.ID, domain.StatusDraft, domain.Launch{StartedAt: started}); ok {
		t.Fatal("a launched campaign cannot launch again")
	}
	due, err := repo.ListAwaitingWinner(ctx, time.Now().UTC(), 10)
	if err != nil || len(due) != 1 {
		t.Fatalf("awaiting winner = %d %v", len(due), err)
	}
	if ok, err := repo.SetWinner(ctx, c.ID, domain.VariantB); err != nil || !ok {
		t.Fatalf("SetWinner = %v %v", ok, err)
	}
	if ok, _ := repo.SetWinner(ctx, c.ID, domain.VariantA); ok {
		t.Fatal("the winner is set once")
	}
	got, _ = repo.Get(ctx, c.WorkspaceID, c.ID)
	if got.ABTest.Winner != domain.VariantB || got.Status != domain.StatusRunning {
		t.Fatalf("after launch = %+v", got)
	}
	if err := repo.Update(ctx, got, got.Status); err != domain.ErrNotEditable {
		t.Fatalf("Update of a running campaign = %v", err)
	}
}

func TestRecipientsAndMetricsAgainstPostgres(t *testing.T) {
	db := migrated(t)
	ctx := context.Background()
	ws, campaignID := uuid.NewString(), uuid.NewString()
	recipients := NewRecipientRepository(db)
	var rows []domain.Recipient
	for i := 0; i < 2500; i++ {
		rows = append(rows, domain.Recipient{Address: emailaddress.Address(uuid.NewString()[:8] + "@gmail.com"), Status: domain.RecipientPending, Variant: domain.VariantWinner})
	}
	rows = append(rows,
		domain.Recipient{Address: "a@outlook.com", Status: domain.RecipientPending, Variant: domain.VariantA},
		domain.Recipient{Address: "b@empresa.com.br", Status: domain.RecipientPending, Variant: domain.VariantB},
		domain.Recipient{Status: domain.RecipientSkipped, SkipReason: domain.SkipNoEmail},
		domain.Recipient{Status: domain.RecipientSkipped, SkipReason: domain.SkipNoEmail},
		domain.Recipient{Address: "a@outlook.com", Status: domain.RecipientSkipped, SkipReason: domain.SkipDuplicate},
	)
	if err := recipients.Replace(ctx, ws, campaignID, rows); err != nil {
		t.Fatal(err)
	}
	if err := recipients.Replace(ctx, ws, campaignID, rows); err != nil {
		t.Fatalf("replacing again must not collide: %v", err)
	}
	counts, err := recipients.Counts(ctx, campaignID)
	if err != nil || counts.Total != 2505 || counts.Pending != 2502 || counts.Skipped[domain.SkipNoEmail] != 2 {
		t.Fatalf("counts = %+v err = %v", counts, err)
	}
	pendingAB, err := recipients.PendingIDs(ctx, campaignID, []domain.Variant{domain.VariantA, domain.VariantB}, "", 10)
	if err != nil || len(pendingAB) != 2 {
		t.Fatalf("pending a/b = %v %v", pendingAB, err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	messages := map[string]schema.EmailMessage{}
	for i, id := range pendingAB {
		rc, _ := recipients.Get(ctx, id)
		m := schema.EmailMessage{
			ID: uuid.NewString(), WorkspaceID: ws, ProviderMessageID: schema.OptionalText(uuid.NewString()), RunKind: "campaign", RunID: campaignID,
			RecipientID: id, DomainID: uuid.NewString(), Address: string(rc.Address), Status: "delivered", SentAt: &now, DeliveredAt: &now,
			CreatedAt: now, UpdatedAt: now,
		}
		if i == 0 {
			m.FirstOpenAt, m.FirstClickAt, m.Opens, m.Clicks = &now, &now, 2, 1
		} else {
			m.BouncedAt, m.BounceKind, m.DeliveredAt, m.Status = &now, "hard", nil, "bounced"
		}
		if err := db.Create(&m).Error; err != nil {
			t.Fatal(err)
		}
		messages[id] = m
		if ok, err := recipients.MarkSent(ctx, id, m.ID, now); err != nil || !ok {
			t.Fatalf("MarkSent = %v %v", ok, err)
		}
	}
	clickOf := func(m schema.EmailMessage, bot bool, link string) schema.EmailMessageEvent {
		return schema.EmailMessageEvent{ID: uuid.NewString(), MessageID: m.ID, WorkspaceID: ws, ProviderMessageID: string(m.ProviderMessageID),
			Kind: "click", OccurredAt: now.Add(time.Duration(len(link)) * time.Second), LinkHash: uuid.NewString(), Link: link, IsBot: bot, CreatedAt: now}
	}
	opened := messages[pendingAB[0]]
	for _, ev := range []schema.EmailMessageEvent{clickOf(opened, false, "https://loja.com.br/a"), clickOf(opened, false, "https://loja.com.br/ab"), clickOf(opened, true, "https://loja.com.br/abc")} {
		if err := db.Create(&ev).Error; err != nil {
			t.Fatal(err)
		}
	}
	metrics := NewMetricsRepository(db)
	tally, err := metrics.Tally(ctx, ws, campaignID)
	if err != nil {
		t.Fatal(err)
	}
	if tally.Sent != 2 || tally.Delivered != 1 || tally.Opens != 1 || tally.Clicks != 1 || tally.HardBounces != 1 || tally.Pending != 2500 || tally.Recipients != 2505 {
		t.Fatalf("tally = %+v", tally)
	}
	if other, _ := metrics.Tally(ctx, uuid.NewString(), campaignID); other.Sent != 0 {
		t.Fatal("metrics are scoped to the workspace")
	}
	variants, err := metrics.VariantResults(ctx, ws, campaignID)
	if err != nil || len(variants) != 2 {
		t.Fatalf("variants = %+v err = %v", variants, err)
	}
	timeline, err := metrics.Timeline(ctx, ws, campaignID, domain.BucketHour)
	if err != nil || len(timeline) != 1 || timeline[0].Sent != 2 || timeline[0].Bounces != 1 || timeline[0].Opens != 1 {
		t.Fatalf("timeline = %+v err = %v", timeline, err)
	}
	links, err := metrics.Links(ctx, ws, campaignID, 10)
	if err != nil || len(links) != 2 || links[0].UniqueClickers != 1 {
		t.Fatalf("links = %+v err = %v (bot clicks are excluded)", links, err)
	}
	domains, err := metrics.Domains(ctx, ws, campaignID)
	if err != nil {
		t.Fatal(err)
	}
	grouped := domain.GroupDomains(domains)
	if len(grouped) != 2 {
		t.Fatalf("domains = %+v", grouped)
	}
	list, total, err := recipients.List(ctx, ws, campaignID, domain.RecipientFilter{Status: domain.RecipientSent}, 0, 10)
	if err != nil || total != 2 || len(list) != 2 || list[0].MessageStatus == "" {
		t.Fatalf("list = %+v total = %d err = %v", list, total, err)
	}
	overview, err := metrics.Overview(ctx, ws, now.Add(-24*time.Hour), 5)
	if err != nil || overview.Totals.Sent != 2 || len(overview.Daily) != 1 {
		t.Fatalf("overview = %+v err = %v", overview, err)
	}
}

func TestSubscriptionsAgainstPostgres(t *testing.T) {
	db := migrated(t)
	ctx := context.Background()
	ws := uuid.NewString()
	subs := NewSubscriptionRepository(db)
	now := time.Now().UTC().Truncate(time.Second)
	sub, err := emailconsent.NewSubscription(emailconsent.Consent{WorkspaceID: ws, Address: "ana@x.com", Source: emailconsent.SourceForm, IP: "203.0.113.9", At: now}, now)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := subs.Save(ctx, sub)
	if err != nil || saved.ID == "" || saved.ConsentIP != "203.0.113.9" {
		t.Fatalf("saved = %+v err = %v", saved, err)
	}
	gone, _, _ := saved.Unsubscribe(emailconsent.UnsubscribeOneClick, now)
	if _, err := subs.Save(ctx, gone); err != nil {
		t.Fatal(err)
	}
	found, err := subs.Find(ctx, ws, []emailaddress.Address{"ana@x.com", "nobody@x.com"})
	if err != nil || len(found) != 1 || found["ana@x.com"].Status != emailconsent.StatusUnsubscribed {
		t.Fatalf("found = %+v err = %v", found, err)
	}
	if _, err := subs.Get(ctx, ws, "nobody@x.com"); err != emailconsent.ErrSubscriptionNotFound {
		t.Fatalf("err = %v", err)
	}
	if db.Migrator().HasTable("email_address_checks") {
		t.Fatal("the list validation table is not migrated any more")
	}
}

func TestAReadyCampaignIsSavedBackAsDraftAndDeletable(t *testing.T) {
	repo, ctx := NewCampaignRepository(migrated(t)), context.Background()
	c := &domain.Campaign{WorkspaceID: uuid.NewString(), Name: "Pronta", Status: domain.StatusDraft, CreatedBy: uuid.NewString()}
	if err := repo.Create(ctx, c); err != nil {
		t.Fatal(err)
	}
	if ok, err := repo.SwapStatus(ctx, c.ID, domain.StatusDraft, domain.StatusReady, domain.HoldNone); err != nil || !ok {
		t.Fatalf("ready = %v, %v", ok, err)
	}
	got, _ := repo.Get(ctx, c.WorkspaceID, c.ID)
	got.Name, got.Status = "Editada", domain.StatusReady.AfterEdit()
	if err := repo.Update(ctx, got, domain.StatusScheduled); err != domain.ErrNotEditable {
		t.Fatalf("an edit from a stale status = %v", err)
	}
	if err := repo.Update(ctx, got, domain.StatusReady); err != nil {
		t.Fatal(err)
	}
	edited, _ := repo.Get(ctx, c.WorkspaceID, c.ID)
	if edited.Status != domain.StatusDraft || edited.Name != "Editada" {
		t.Fatalf("edited = %+v", edited)
	}
	if ok, _ := repo.SwapStatus(ctx, c.ID, domain.StatusDraft, domain.StatusReady, domain.HoldNone); !ok {
		t.Fatal("ready again")
	}
	if err := repo.Delete(ctx, c.WorkspaceID, c.ID); err != nil {
		t.Fatalf("delete a ready campaign = %v", err)
	}
}
