package notification_usecase

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"vozko/domain/notification"
	"vozko/domain/workspace"
)

type centerRig struct {
	store     *memoryCenter
	queue     *recordingQueue
	images    *ownedImages
	pusher    *recordingPusher
	emails    *recordingNotifier
	clock     *steppingClock
	publisher *Publisher
	delivery  *Delivery
	sweeper   *Sweeper
}

func teamDirectory() fixedDirectory {
	return fixedDirectory{members: map[string][]notification.Member{
		"ws-1": {
			{UserID: "owner", Email: "owner@x.com", Name: "Ana", Manager: true},
			{UserID: "agent", Email: "agent@x.com", Name: "Beto"},
			{UserID: "seller", Email: "seller@x.com", Name: "Caio"},
			{UserID: "noemail", Email: "", Name: "Duda"},
		},
	}}
}

func newCenterRig(t *testing.T, directory fixedDirectory, granted grantedPermissions) *centerRig {
	t.Helper()
	rig := &centerRig{
		store: newMemoryCenter(), queue: &recordingQueue{}, images: &ownedImages{},
		pusher: &recordingPusher{}, emails: &recordingNotifier{}, clock: newClock(),
	}
	var err error
	rig.publisher, err = NewPublisher(PublisherDeps{Outbox: rig.store, Queue: rig.queue, Images: rig.images, Episodes: newMemoryEpisodes(), Now: rig.clock.now})
	if err != nil {
		t.Fatal(err)
	}
	rig.delivery, err = NewDelivery(DeliveryDeps{
		Outbox: rig.store, Directory: directory, Permissions: granted, Preferences: rig.store,
		Pusher: rig.pusher, Email: rig.emails, Digests: rig.store, DashboardURL: "https://app.vozko.test/", Now: rig.clock.now,
	})
	if err != nil {
		t.Fatal(err)
	}
	rig.sweeper, err = NewSweeper(SweeperDeps{Outbox: rig.store, Broadcasts: rig.store, Queue: rig.queue, Now: rig.clock.now})
	if err != nil {
		t.Fatal(err)
	}
	return rig
}

func campaignDraft() notification.Draft {
	return notification.Draft{
		WorkspaceID: "ws-1", Category: notification.CategoryCampaigns, Severity: notification.SeverityWarning,
		Title: "Campanha pausada", Body: "O modelo foi pausado pela Meta.", Audience: notification.Everyone(),
		ActionURL: "/dashboard/whatsapp-campaigns/c-1", ActionLabel: "Ver campanha",
	}
}

func TestPublishStoresAPendingRecordAndQueuesItsFanout(t *testing.T) {
	rig := newCenterRig(t, teamDirectory(), nil)
	rec, err := rig.publisher.Publish(context.Background(), campaignDraft())
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if rec.ID == "" || rec.Status != notification.StatusPending || rec.CreatedAt.IsZero() {
		t.Fatalf("record = %+v", rec)
	}
	if !reflect.DeepEqual(rig.queue.fanouts, []string{rec.ID}) {
		t.Fatalf("fanouts = %v", rig.queue.fanouts)
	}
}

func TestPublishRefusesInvalidDraftsAndForeignImagesWithoutStoring(t *testing.T) {
	rig := newCenterRig(t, teamDirectory(), nil)
	broken := campaignDraft()
	broken.Title = ""
	if _, err := rig.publisher.Publish(context.Background(), broken); !errors.Is(err, notification.ErrTitleRequired) {
		t.Fatalf("blank title = %v", err)
	}
	foreign := campaignDraft()
	foreign.ImageURL = "https://tracker.example/pixel.png"
	if _, err := rig.publisher.Publish(context.Background(), foreign); !errors.Is(err, notification.ErrImageForeign) {
		t.Fatalf("foreign image = %v", err)
	}
	owned := campaignDraft()
	owned.ImageURL = ownedImagePrefix + "1.png"
	if _, err := rig.publisher.Publish(context.Background(), owned); err != nil {
		t.Fatalf("an uploaded image must pass: %v", err)
	}
	if len(rig.store.records) != 1 {
		t.Fatalf("only the valid draft is stored, got %d", len(rig.store.records))
	}
}

func TestPublishingTheSameDedupKeyTwiceReturnsTheFirstWithoutQueueingAgain(t *testing.T) {
	rig := newCenterRig(t, teamDirectory(), nil)
	d := campaignDraft()
	d.DedupKey = "monthly_invoice:inv-1"
	first, _ := rig.publisher.Publish(context.Background(), d)
	second, err := rig.publisher.Publish(context.Background(), d)
	if err != nil || second.ID != first.ID {
		t.Fatalf("second = %+v, %v; want the first record", second, err)
	}
	if len(rig.queue.fanouts) != 1 {
		t.Fatalf("a repeat must not queue another fanout, got %v", rig.queue.fanouts)
	}
}

func TestPublishKeepsTheRecordWhenQueueingFailsSoTheSweeperDeliversIt(t *testing.T) {
	rig := newCenterRig(t, teamDirectory(), nil)
	rig.queue.err = errBoom
	rec, err := rig.publisher.Publish(context.Background(), campaignDraft())
	if err != nil {
		t.Fatalf("a stored notification is not lost when the queue is down: %v", err)
	}
	rig.queue.err = nil
	rig.clock.at = rig.clock.at.Add(StaleAfter + time.Minute)
	if err := rig.sweeper.Sweep(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if !reflect.DeepEqual(rig.queue.fanouts, []string{rec.ID}) {
		t.Fatalf("the sweeper must requeue the stale pending record, got %v", rig.queue.fanouts)
	}
}

func TestSweepRequeuesStaleQueuedBroadcasts(t *testing.T) {
	rig := newCenterRig(t, teamDirectory(), nil)
	b, _ := rig.store.CreateBroadcast(context.Background(), notification.Broadcast{Status: notification.BroadcastQueued, CreatedAt: rig.clock.now()})
	rig.clock.at = rig.clock.at.Add(StaleAfter + time.Minute)
	if err := rig.sweeper.Sweep(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if !reflect.DeepEqual(rig.queue.broadcasts, []string{b.ID}) {
		t.Fatalf("broadcasts requeued = %v", rig.queue.broadcasts)
	}
}

func TestDeliverWritesOneRowPerSelectedMemberPushesAndMarksDelivered(t *testing.T) {
	granted := grantedPermissions{"ws-1/owner/whatsapp_campaigns:update": true, "ws-1/seller/whatsapp_campaigns:update": true}
	rig := newCenterRig(t, teamDirectory(), granted)
	d := campaignDraft()
	d.Audience = notification.Holding(workspace.ResourceWhatsAppCampaigns, workspace.ActionUpdate)
	rec, _ := rig.publisher.Publish(context.Background(), d)

	if err := rig.delivery.Deliver(context.Background(), rec.ID); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if got := rig.store.recipientsOf(rec.ID); !reflect.DeepEqual(got, []string{"owner", "seller"}) {
		t.Fatalf("recipients = %v", got)
	}
	if len(rig.pusher.batches) != 1 || rig.pusher.batches[0].item.ID != rec.ID || len(rig.pusher.batches[0].userIDs) != 2 {
		t.Fatalf("pushes = %+v", rig.pusher.batches)
	}
	stored, _ := rig.store.Get(context.Background(), rec.ID)
	if stored.Status != notification.StatusDelivered || stored.RecipientCount != 2 || stored.DeliveredAt == nil {
		t.Fatalf("stored = %+v", stored)
	}
	if len(rig.emails.sent) != 0 {
		t.Fatal("no email when the draft did not ask for one")
	}
}

func TestDeliverTwiceIsHarmless(t *testing.T) {
	rig := newCenterRig(t, teamDirectory(), nil)
	d := campaignDraft()
	d.Email = true
	rec, _ := rig.publisher.Publish(context.Background(), d)
	_ = rig.delivery.Deliver(context.Background(), rec.ID)
	if err := rig.delivery.Deliver(context.Background(), rec.ID); err != nil {
		t.Fatalf("second deliver: %v", err)
	}
	if len(rig.pusher.batches) != 1 || len(rig.emails.sent) != 3 {
		t.Fatalf("a delivered record is not delivered again: pushes=%d emails=%d", len(rig.pusher.batches), len(rig.emails.sent))
	}
}

func TestDeliverEmailsRespectMutedCategoriesButCriticalAlwaysArrives(t *testing.T) {
	rig := newCenterRig(t, teamDirectory(), nil)
	rig.store.prefs["ws-1/agent"] = notification.Preferences{EmailMuted: []notification.Category{notification.CategoryCampaigns}}

	d := campaignDraft()
	d.Email = true
	rec, _ := rig.publisher.Publish(context.Background(), d)
	if err := rig.delivery.Deliver(context.Background(), rec.ID); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if got := rig.emails.emails(); !reflect.DeepEqual(got, []string{"owner@x.com", "seller@x.com"}) {
		t.Fatalf("emails = %v; the muted agent and the member without email get none", got)
	}

	critical := campaignDraft()
	critical.Email, critical.Severity, critical.Title = true, notification.SeverityCritical, "Campanha encerrada"
	rec2, _ := rig.publisher.Publish(context.Background(), critical)
	_ = rig.delivery.Deliver(context.Background(), rec2.ID)
	if got := rig.emails.emails(); len(got) != 5 {
		t.Fatalf("critical reaches the muted agent too, got %v", got)
	}
}

func TestDeliverHoldsNonCriticalEmailsOfDigestReadersAndSendsTheRestAtOnce(t *testing.T) {
	rig := newCenterRig(t, teamDirectory(), nil)
	rig.store.prefs["ws-1/agent"] = notification.Preferences{EmailDigest: true}

	d := campaignDraft()
	d.Email = true
	rec, _ := rig.publisher.Publish(context.Background(), d)
	if err := rig.delivery.Deliver(context.Background(), rec.ID); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	if got := rig.emails.emails(); !reflect.DeepEqual(got, []string{"owner@x.com", "seller@x.com"}) {
		t.Fatalf("emails = %v; the digest reader waits for the morning", got)
	}
	if rig.store.heldFor("agent") != 1 || rig.store.heldFor("owner") != 0 {
		t.Fatal("only the digest reader's row is held for the digest")
	}

	critical := campaignDraft()
	critical.Email, critical.Severity, critical.Title = true, notification.SeverityCritical, "Campanha encerrada"
	rec2, _ := rig.publisher.Publish(context.Background(), critical)
	_ = rig.delivery.Deliver(context.Background(), rec2.ID)
	if got := rig.emails.emails(); len(got) != 5 || rig.store.heldFor("agent") != 1 {
		t.Fatalf("critical emails never wait: %v", got)
	}
}

func TestDeliverEmailUsesTheGenericTemplateWithAbsoluteLinks(t *testing.T) {
	rig := newCenterRig(t, teamDirectory(), nil)
	d := campaignDraft()
	d.Email, d.Audience = true, notification.Users("owner")
	rec, _ := rig.publisher.Publish(context.Background(), d)
	_ = rig.delivery.Deliver(context.Background(), rec.ID)

	sent := rig.emails.sent[0]
	if sent.Template != GenericEmailTemplate || sent.Subject != "Campanha pausada" || sent.DedupKey != rec.EmailDedupKey("owner") {
		t.Fatalf("sent = %+v", sent)
	}
	p := sent.Placeholders
	if p["ActionURL"] != "https://app.vozko.test/dashboard/whatsapp-campaigns/c-1" || p["InboxURL"] != "https://app.vozko.test/dashboard" {
		t.Fatalf("links must be absolute: %v / %v", p["ActionURL"], p["InboxURL"])
	}
	if p["Tone"] != "warning" || p["CategoryLabel"] != "Campanhas" || p["WorkspaceName"] != "Loja ws-1" || p["Headline"] != "Campanha pausada" {
		t.Fatalf("placeholders = %v", p)
	}
	if !strings.Contains(p["SentAt"].(string), "/10/2026") {
		t.Fatalf("SentAt = %v", p["SentAt"])
	}
}

func TestDeliverEmailUsesTheProducersOwnCopyWhenGiven(t *testing.T) {
	rig := newCenterRig(t, teamDirectory(), nil)
	d := campaignDraft()
	d.Email, d.Audience = true, notification.Managers()
	d.EmailCopy = &notification.EmailCopy{Template: "whatsapp_campaign_hold.html", Subject: "Campanha pausada: Promo", Placeholders: map[string]interface{}{"Wave": "3"}}
	rec, _ := rig.publisher.Publish(context.Background(), d)
	_ = rig.delivery.Deliver(context.Background(), rec.ID)

	sent := rig.emails.sent[0]
	if sent.Template != "whatsapp_campaign_hold.html" || sent.Subject != "Campanha pausada: Promo" || sent.Placeholders["Wave"] != "3" {
		t.Fatalf("sent = %+v", sent)
	}
}

func TestDeliverFailsClosedWhenPreferencesCannotBeReadAndRetriesWithoutDuplicates(t *testing.T) {
	rig := newCenterRig(t, teamDirectory(), nil)
	d := campaignDraft()
	d.Email = true
	rec, _ := rig.publisher.Publish(context.Background(), d)

	rig.store.prefsErr = errBoom
	if err := rig.delivery.Deliver(context.Background(), rec.ID); !errors.Is(err, errBoom) {
		t.Fatalf("unknown preferences must fail the delivery, got %v", err)
	}
	if len(rig.emails.sent) != 0 {
		t.Fatal("no email may go out while preferences are unknown")
	}
	if stored, _ := rig.store.Get(context.Background(), rec.ID); stored.Status != notification.StatusPending {
		t.Fatal("the record stays pending so the retry runs")
	}

	rig.store.prefsErr = nil
	if err := rig.delivery.Deliver(context.Background(), rec.ID); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if got := rig.store.recipientsOf(rec.ID); len(got) != 4 {
		t.Fatalf("recipients after retry = %v", got)
	}
}

func TestDeliverPropagatesEmailQueueFailures(t *testing.T) {
	rig := newCenterRig(t, teamDirectory(), nil)
	rig.emails.err = errBoom
	d := campaignDraft()
	d.Email = true
	rec, _ := rig.publisher.Publish(context.Background(), d)
	if err := rig.delivery.Deliver(context.Background(), rec.ID); !errors.Is(err, errBoom) {
		t.Fatalf("deliver = %v, want the email failure", err)
	}
}

func TestDeliverOfAnUnknownRecordIsARefusal(t *testing.T) {
	rig := newCenterRig(t, teamDirectory(), nil)
	err := rig.delivery.Deliver(context.Background(), "missing")
	if !errors.Is(err, notification.ErrNotFound) || !notification.IsRefusal(err) {
		t.Fatalf("deliver = %v", err)
	}
}

func TestDeliverWithNobodySelectedStillCompletes(t *testing.T) {
	rig := newCenterRig(t, teamDirectory(), nil)
	d := campaignDraft()
	d.Audience = notification.Users("stranger")
	rec, _ := rig.publisher.Publish(context.Background(), d)
	if err := rig.delivery.Deliver(context.Background(), rec.ID); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	stored, _ := rig.store.Get(context.Background(), rec.ID)
	if stored.Status != notification.StatusDelivered || stored.RecipientCount != 0 || len(rig.pusher.batches) != 0 {
		t.Fatalf("stored = %+v pushes = %d", stored, len(rig.pusher.batches))
	}
}

func TestDeliverPushesInChunks(t *testing.T) {
	members := make([]notification.Member, PushChunk+3)
	for i := range members {
		members[i] = notification.Member{UserID: fmt.Sprintf("u-%04d", i)}
	}
	rig := newCenterRig(t, fixedDirectory{members: map[string][]notification.Member{"ws-1": members}}, nil)
	rec, _ := rig.publisher.Publish(context.Background(), campaignDraft())
	_ = rig.delivery.Deliver(context.Background(), rec.ID)
	if len(rig.pusher.batches) != 2 || len(rig.pusher.batches[0].userIDs) != PushChunk || len(rig.pusher.batches[1].userIDs) != 3 {
		t.Fatalf("batches = %d", len(rig.pusher.batches))
	}
}

func TestConstructorsRefuseMissingDependencies(t *testing.T) {
	if _, err := NewPublisher(PublisherDeps{}); err == nil {
		t.Fatal("publisher without dependencies must not build")
	}
	if _, err := NewDelivery(DeliveryDeps{}); err == nil {
		t.Fatal("delivery without dependencies must not build")
	}
	if _, err := NewSweeper(SweeperDeps{}); err == nil {
		t.Fatal("sweeper without dependencies must not build")
	}
}

func TestRetentionPurgesOldNotificationsInBatchesUntilDone(t *testing.T) {
	store := newMemoryCenter()
	clock := newClock()
	old := clock.at.Add(-RetentionPeriod - time.Hour)
	for i := 0; i < 5; i++ {
		_, _, _ = store.Create(context.Background(), notification.Record{Draft: notification.Draft{WorkspaceID: "ws-1"}, CreatedAt: old})
	}
	_, _, _ = store.Create(context.Background(), notification.Record{Draft: notification.Draft{WorkspaceID: "ws-1"}, CreatedAt: clock.at})

	retention := NewRetention(store, newMemoryEpisodes(), 2, clock.now)
	if err := retention.Purge(context.Background()); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if len(store.records) != 1 || store.purgeBudget != 5 {
		t.Fatalf("left %d records after purging %d", len(store.records), store.purgeBudget)
	}
}
