package carriercampaign_repository

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/campaign"
	"vozko/domain/carriercampaign"
	"vozko/domain/carriermessaging"
	"vozko/domain/shared"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

const racers = 12

func integrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := repotest.IsolatedDB(t, "carrier_campaign_test", append(schema.CarrierCampaignModels(), schema.CarrierMessagingModels()...)...)
	for _, ddl := range []string{
		`CREATE TABLE balance_transactions (id uuid PRIMARY KEY, workspace_id uuid NOT NULL, type varchar(20) NOT NULL, amount bigint NOT NULL,
			service_type varchar(50) NOT NULL, reference_id varchar(255), is_refund boolean NOT NULL DEFAULT false, created_at timestamptz NOT NULL)`,
		`CREATE TABLE opportunities (id uuid PRIMARY KEY, workspace_id uuid NOT NULL, lead_id uuid, status varchar(10) NOT NULL,
			value_cents bigint NOT NULL DEFAULT 0, close_date timestamptz, deleted_at timestamptz)`,
		`CREATE TABLE whatsapp_campaigns (id uuid PRIMARY KEY, workspace_id uuid NOT NULL)`,
		`CREATE TABLE whatsapp_campaign_entries (id uuid PRIMARY KEY, campaign_id uuid NOT NULL, lead_id uuid NOT NULL,
			delivered_at timestamptz, deleted_at timestamptz)`,
		`CREATE TABLE leads (id uuid PRIMARY KEY, workspace_id uuid NOT NULL, name varchar(255), deleted_at timestamptz)`,
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatalf("ddl: %v", err)
		}
	}
	return db
}

func runConcurrently(n int, fn func(i int)) {
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			fn(i)
		}()
	}
	close(start)
	wg.Wait()
}

func seedCampaign(t *testing.T, repo carriercampaign.CampaignRepository, ws string, mutate func(*carriercampaign.Campaign)) *carriercampaign.Campaign {
	t.Helper()
	c := sampleCampaign()
	c.WorkspaceID, c.SenderID, c.CreatedBy = ws, uuid.NewString(), uuid.NewString()
	if mutate != nil {
		mutate(c)
	}
	if err := repo.Create(context.Background(), c); err != nil {
		t.Fatalf("seed campaign: %v", err)
	}
	return c
}

func TestIntegrationCampaignLifecycle(t *testing.T) {
	db := integrationDB(t)
	repo := NewCampaignRepository(db)
	ctx := context.Background()
	ws := uuid.NewString()
	c := seedCampaign(t, repo, ws, func(c *carriercampaign.Campaign) {
		c.Recurrence = &carriercampaign.Recurrence{Frequency: carriercampaign.FrequencyWeekly, Time: "09:30", Weekdays: []time.Weekday{time.Monday}, Timezone: "America/Sao_Paulo"}
	})
	got, err := repo.FindByID(ctx, ws, c.ID)
	if err != nil || got.Recurrence == nil || got.Recurrence.Weekdays[0] != time.Monday || got.Audience.Fingerprint != "fp" || got.Fallback.SMS.RouteID != "r1" || got.Variants[0].Content.Text != "oi" {
		t.Fatalf("round trip %+v %v", got, err)
	}
	if _, err := repo.FindByID(ctx, uuid.NewString(), c.ID); !errors.Is(err, carriercampaign.ErrCampaignNotFound) {
		t.Fatalf("other workspace: %v", err)
	}

	c.Name, c.Status, c.Recurrence = "Renamed", carriercampaign.StatusScheduled, nil
	if err := repo.Update(ctx, c, carriercampaign.StatusDraft); err != nil {
		t.Fatal(err)
	}
	if got, err = repo.FindByIDUnscoped(ctx, c.ID); err != nil || got.Name != "Renamed" || got.Recurrence != nil || got.Status != carriercampaign.StatusScheduled {
		t.Fatalf("after update %+v %v", got, err)
	}

	due := time.Now().UTC().Add(-time.Minute)
	if moved, err := repo.Reschedule(ctx, c.ID, carriercampaign.StatusScheduled, carriercampaign.StatusScheduled, &due); err != nil || !moved {
		t.Fatalf("reschedule %v %v", moved, err)
	}
	list, err := repo.ListScheduledDue(ctx, time.Now().UTC(), 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("due %v %v", list, err)
	}
	var claimed atomic.Int64
	runConcurrently(racers, func(int) {
		if ok, err := repo.ClaimScheduledStart(c.ID, time.Now().UTC()); err == nil && ok {
			claimed.Add(1)
		}
	})
	if claimed.Load() != 1 {
		t.Fatalf("%d racers claimed the scheduled start", claimed.Load())
	}
	if moved, err := repo.SwapStatus(ctx, c.ID, carriercampaign.StatusScheduled, carriercampaign.StatusRunning); err != nil || !moved {
		t.Fatalf("start %v %v", moved, err)
	}
	c.Status = carriercampaign.StatusScheduled
	if err := repo.Update(ctx, c, carriercampaign.StatusScheduled); !errors.Is(err, carriercampaign.ErrNotEditable) {
		t.Fatalf("running campaigns are not editable: %v", err)
	}
	if err := repo.Delete(ctx, ws, c.ID); !errors.Is(err, carriercampaign.ErrNotEditable) {
		t.Fatalf("running campaigns are not deletable: %v", err)
	}
	draft := seedCampaign(t, repo, ws, nil)
	if err := repo.Delete(ctx, ws, draft.ID); err != nil {
		t.Fatal(err)
	}
	counts, err := repo.CountByStatus(ctx, ws)
	if err != nil || counts[carriercampaign.StatusRunning] != 1 || len(counts) != 1 {
		t.Fatalf("counts %v %v", counts, err)
	}
}

func TestIntegrationReadyCampaign(t *testing.T) {
	db := integrationDB(t)
	repo := NewCampaignRepository(db)
	ctx := context.Background()
	ws := uuid.NewString()
	c := seedCampaign(t, repo, ws, func(c *carriercampaign.Campaign) { c.ScheduledAt = nil })
	if moved, err := repo.SwapStatus(ctx, c.ID, carriercampaign.StatusDraft, carriercampaign.StatusReady); err != nil || !moved {
		t.Fatalf("ready %v %v", moved, err)
	}
	past := time.Now().UTC().Add(-time.Hour)
	if err := db.Model(&schema.CarrierCampaign{}).Where("id = ?", c.ID).Update("scheduled_at", past).Error; err != nil {
		t.Fatal(err)
	}
	if due, err := repo.ListScheduledDue(ctx, time.Now().UTC(), 10); err != nil || len(due) != 0 {
		t.Fatalf("a ready campaign is never due: %v %v", due, err)
	}
	if claimed, err := repo.ClaimScheduledStart(c.ID, time.Now().UTC()); err != nil || claimed {
		t.Fatalf("a ready campaign is never claimed: %v %v", claimed, err)
	}
	counts, err := repo.CountByStatus(ctx, ws)
	if err != nil || counts[carriercampaign.StatusReady] != 1 {
		t.Fatalf("counts %v %v", counts, err)
	}

	stale := *c
	stale.Name, stale.Status = "Edited", carriercampaign.StatusDraft
	if err := repo.Update(ctx, &stale, carriercampaign.StatusDraft); !errors.Is(err, carriercampaign.ErrNotEditable) {
		t.Fatalf("an edit that read draft must not overwrite a ready campaign: %v", err)
	}
	if err := repo.Update(ctx, &stale, carriercampaign.StatusReady); err != nil {
		t.Fatal(err)
	}
	got, err := repo.FindByID(ctx, ws, c.ID)
	if err != nil || got.Status != carriercampaign.StatusDraft || got.Name != "Edited" {
		t.Fatalf("an edited ready campaign goes back to draft: %+v %v", got, err)
	}

	if moved, err := repo.SwapStatus(ctx, c.ID, carriercampaign.StatusDraft, carriercampaign.StatusReady); err != nil || !moved {
		t.Fatalf("ready again %v %v", moved, err)
	}
	if err := repo.Delete(ctx, ws, c.ID); err != nil {
		t.Fatalf("a ready campaign deletes like a draft: %v", err)
	}
	if _, err := repo.FindByID(ctx, ws, c.ID); !errors.Is(err, carriercampaign.ErrCampaignNotFound) {
		t.Fatalf("deleted %v", err)
	}
}

func TestIntegrationCampaignList(t *testing.T) {
	db := integrationDB(t)
	repo := NewCampaignRepository(db)
	ctx := context.Background()
	ws := uuid.NewString()
	seedCampaign(t, repo, ws, func(c *carriercampaign.Campaign) { c.Name = "Promo 50% off" })
	seedCampaign(t, repo, ws, func(c *carriercampaign.Campaign) { c.Name = "Promo 500 off" })
	seedCampaign(t, repo, ws, func(c *carriercampaign.Campaign) {
		c.Name, c.Channel, c.Fallback = "Promo 50% sms", carriermessaging.ChannelSMS, carriercampaign.Fallback{}
		c.Variants = []carriercampaign.Variant{{Name: "A", Text: "oi"}}
	})
	seedCampaign(t, repo, uuid.NewString(), func(c *carriercampaign.Campaign) { c.Name = "Promo 50% elsewhere" })
	page, err := repo.List(ctx, carriercampaign.ListCampaignsInput{WorkspaceID: ws, Search: "50%", Channel: carriermessaging.ChannelRCS, Pagination: shared.Pagination{Page: 1, PageSize: 10}})
	if err != nil || page.TotalItems != 1 || page.Items[0].Name != "Promo 50% off" {
		t.Fatalf("list %+v %v", page, err)
	}
	all, err := repo.List(ctx, carriercampaign.ListCampaignsInput{WorkspaceID: ws, Pagination: shared.Pagination{Page: 2, PageSize: 2}})
	if err != nil || all.TotalItems != 3 || len(all.Items) != 1 || all.TotalPages != 2 {
		t.Fatalf("paged %+v %v", all, err)
	}
}

func TestIntegrationRunsAreOnePerOccurrence(t *testing.T) {
	db := integrationDB(t)
	repo := NewRunRepository(db)
	ctx := context.Background()
	cid := uuid.NewString()
	occurrence := time.Now().UTC().Truncate(time.Microsecond)
	var created atomic.Int64
	runConcurrently(racers, func(int) {
		ok, err := repo.Create(ctx, &carriercampaign.Run{WorkspaceID: uuid.NewString(), CampaignID: cid, Occurrence: occurrence, Status: campaign.StatusRunning, StartedAt: occurrence})
		if err == nil && ok {
			created.Add(1)
		}
	})
	if created.Load() != 1 {
		t.Fatalf("%d runs created for one occurrence", created.Load())
	}
	run, err := repo.FindByOccurrence(ctx, cid, occurrence)
	if err != nil {
		t.Fatal(err)
	}
	if active, err := repo.FindActive(ctx, cid); err != nil || active.ID != run.ID {
		t.Fatalf("active %+v %v", active, err)
	}
	if done, err := repo.Complete(ctx, run.ID, occurrence.Add(time.Hour)); err != nil || !done {
		t.Fatalf("complete %v %v", done, err)
	}
	if done, err := repo.Complete(ctx, run.ID, occurrence.Add(time.Hour)); err != nil || done {
		t.Fatalf("second complete %v %v", done, err)
	}
	if _, err := repo.FindActive(ctx, cid); !errors.Is(err, carriercampaign.ErrRunNotFound) {
		t.Fatalf("no active run: %v", err)
	}
	counts, err := repo.CountByStatus(ctx, cid)
	if err != nil || counts[campaign.StatusCompleted] != 1 {
		t.Fatalf("counts %v %v", counts, err)
	}
}

func TestIntegrationRecipients(t *testing.T) {
	db := integrationDB(t)
	repo := NewRecipientRepository(db)
	ctx := context.Background()
	ws, cid, rid := uuid.NewString(), uuid.NewString(), uuid.NewString()
	recipients := make([]carriercampaign.Recipient, recipientBatch+20)
	for i := range recipients {
		recipients[i] = carriercampaign.Recipient{WorkspaceID: ws, CampaignID: cid, RunID: rid, LeadID: uuid.NewString(), Phone: "551199999" + uuid.NewString()[:4], Variant: i % 2, Status: carriercampaign.RecipientPending}
	}
	recipients[0].Status, recipients[0].SkipReason = carriercampaign.RecipientSkipped, carriercampaign.SkipSuppressed
	if err := repo.CreateMany(ctx, recipients); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateMany(ctx, recipients[:10]); err != nil {
		t.Fatalf("a retried batch must be ignored: %v", err)
	}
	counts, err := repo.Counts(ctx, rid)
	if err != nil || counts.Total != int64(len(recipients)) || counts.Skipped[carriercampaign.SkipSuppressed] != 1 || counts.Pending != int64(len(recipients)-1) {
		t.Fatalf("counts %+v %v", counts, err)
	}

	var seen []string
	after := ""
	for {
		ids, err := repo.PendingIDs(ctx, rid, after, 200)
		if err != nil {
			t.Fatal(err)
		}
		if len(ids) == 0 {
			break
		}
		seen = append(seen, ids...)
		after = ids[len(ids)-1]
	}
	if len(seen) != len(recipients)-1 {
		t.Fatalf("paged %d pending ids", len(seen))
	}

	target := seen[0]
	messageID := uuid.NewString()
	var wins atomic.Int64
	runConcurrently(racers, func(i int) {
		var ok bool
		var err error
		switch i % 3 {
		case 0:
			ok, err = repo.MarkSent(ctx, target, messageID, time.Now().UTC())
		case 1:
			ok, err = repo.MarkSkipped(ctx, target, carriercampaign.SkipBalance)
		default:
			ok, err = repo.MarkFailed(ctx, target, "charge_refused")
		}
		if err == nil && ok {
			wins.Add(1)
		}
	})
	if wins.Load() != 1 {
		t.Fatalf("%d settlements of one recipient", wins.Load())
	}
	if _, err := repo.MarkSent(ctx, uuid.NewString(), messageID, time.Now().UTC()); !errors.Is(err, carriercampaign.ErrRecipientNotFound) {
		t.Fatalf("missing recipient: %v", err)
	}
	byVariant, err := repo.CountsByVariant(ctx, rid)
	if err != nil || byVariant[0].Total+byVariant[1].Total != int64(len(recipients)) {
		t.Fatalf("by variant %+v %v", byVariant, err)
	}
	variant := 1
	page, err := repo.List(ctx, carriercampaign.ListRecipientsInput{WorkspaceID: ws, RunID: rid, Variant: &variant, Status: carriercampaign.RecipientPending, Pagination: shared.Pagination{Page: 1, PageSize: 5}})
	if err != nil || len(page.Items) != 5 || page.TotalItems < 250 {
		t.Fatalf("list %+v %v", page, err)
	}
}
