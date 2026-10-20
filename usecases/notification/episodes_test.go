package notification_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/notification"
)

const day = 24 * time.Hour

type episodeRig struct {
	store     *memoryCenter
	episodes  *memoryEpisodes
	queue     *recordingQueue
	emails    *recordingNotifier
	clock     *fixedClock
	publisher *Publisher
	delivery  *Delivery
}

func newEpisodeRig(t *testing.T) *episodeRig {
	t.Helper()
	rig := &episodeRig{
		store: newMemoryCenter(), episodes: newMemoryEpisodes(), queue: &recordingQueue{}, emails: &recordingNotifier{},
		clock: &fixedClock{at: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)},
	}
	var err error
	rig.publisher, err = NewPublisher(PublisherDeps{Outbox: rig.store, Queue: rig.queue, Images: &ownedImages{}, Episodes: rig.episodes, Now: rig.clock.now})
	if err != nil {
		t.Fatal(err)
	}
	rig.delivery, err = NewDelivery(DeliveryDeps{
		Outbox: rig.store, Directory: fixedDirectory{members: map[string][]notification.Member{"ws-1": {{UserID: "owner", Email: "owner@x.com", Manager: true}}}},
		Preferences: rig.store, Pusher: &recordingPusher{}, Email: rig.emails, Digests: rig.store,
		DashboardURL: "https://app.vozko.test", Now: rig.clock.now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return rig
}

func outOfFundsDraft() notification.Draft {
	return notification.Draft{
		WorkspaceID: "ws-1", Category: notification.CategoryAdvertising, Severity: notification.SeverityCritical,
		Title: "Os anúncios pararam: os fundos acabaram", Body: "Os fundos da conta de anúncios Loja na Meta acabaram.",
		ActionURL: "/dashboard/advertising", ActionLabel: "Adicionar fundos na Meta", Audience: notification.Managers(), Email: true,
		EmailCopy:  &notification.EmailCopy{Template: "meta_ads_funds_alert.html", Subject: "Os anúncios pararam: os fundos acabaram - Marca"},
		EpisodeKey: "meta_ads_funds:acc-1:funds_out",
	}
}

func (r *episodeRig) publish(t *testing.T, d notification.Draft) {
	t.Helper()
	if _, err := r.publisher.Publish(context.Background(), d); err != nil {
		t.Fatalf("publish: %v", err)
	}
}

func (r *episodeRig) deliverQueued(t *testing.T) {
	t.Helper()
	for _, id := range r.queue.fanouts {
		if err := r.delivery.Deliver(context.Background(), id); err != nil && !errors.Is(err, notification.ErrNotFound) {
			t.Fatalf("deliver %s: %v", id, err)
		}
	}
	r.queue.fanouts = nil
}

func (r *episodeRig) subjects() []string {
	out := make([]string, 0, len(r.emails.sent))
	for _, sent := range r.emails.sent {
		out = append(out, sent.Subject)
	}
	return out
}

func TestTheFirstPublishOfASituationOpensAnEpisodeAndNotifiesAtOnce(t *testing.T) {
	rig := newEpisodeRig(t)
	rec, err := rig.publisher.Publish(context.Background(), outOfFundsDraft())
	if err != nil {
		t.Fatal(err)
	}
	episodes := rig.episodes.all()
	if len(episodes) != 1 || episodes[0].StepsSent != notification.FirstNotice || episodes[0].NotificationID != rec.ID {
		t.Fatalf("episodes %+v", episodes)
	}
	if rec.Title != outOfFundsDraft().Title || rec.DedupKey != episodes[0].StepKey(notification.FirstNotice) || rec.EpisodeKey != "meta_ads_funds:acc-1:funds_out" {
		t.Fatalf("record %+v", rec)
	}
	if len(rig.queue.fanouts) != 1 {
		t.Fatalf("fanouts %v", rig.queue.fanouts)
	}
}

func TestRepeatedPublishesOfAnOngoingSituationStaySilentUntilTheReminderIsDue(t *testing.T) {
	rig := newEpisodeRig(t)
	rig.publish(t, outOfFundsDraft())
	for i := 0; i < 143; i++ {
		rig.clock.advance(10 * time.Minute)
		rig.publish(t, outOfFundsDraft())
	}
	if len(rig.queue.fanouts) != 1 || len(rig.store.liveRecords()) != 1 {
		t.Fatalf("a day of watch runs must not notify again: fanouts %v", rig.queue.fanouts)
	}
}

func TestTheReminderReplacesTheBellEntryAndSaysSinceWhen(t *testing.T) {
	rig := newEpisodeRig(t)
	rig.publish(t, outOfFundsDraft())
	rig.deliverQueued(t)
	rig.clock.advance(day)
	rig.publish(t, outOfFundsDraft())

	live := rig.store.liveRecords()
	if len(live) != 1 {
		t.Fatalf("the bell keeps one entry per episode, got %d", len(live))
	}
	reminder := live[0]
	if !strings.HasPrefix(reminder.Title, "Lembrete: ") || !strings.Contains(reminder.Body, "18/10/2026 às 09:00") {
		t.Fatalf("reminder %q / %q must say since when in Brasília time", reminder.Title, reminder.Body)
	}
	if len(rig.store.recipients) != 0 {
		t.Fatal("the first notice's recipient rows leave with it, so the unread count does not keep it")
	}
	if episode := rig.episodes.all()[0]; episode.StepsSent != notification.Reminder || episode.NotificationID != reminder.ID {
		t.Fatalf("episode %+v", episode)
	}
}

func TestAnEpisodeSendsThreeEmailsAndThenStaysSilentWhileTheSituationLasts(t *testing.T) {
	rig := newEpisodeRig(t)
	for elapsed := time.Duration(0); elapsed <= 30*day; elapsed += 6 * time.Hour {
		rig.publish(t, outOfFundsDraft())
		rig.deliverQueued(t)
		rig.clock.advance(6 * time.Hour)
	}
	subjects := rig.subjects()
	if len(subjects) != 3 {
		t.Fatalf("emails %v, want three", subjects)
	}
	if !strings.HasPrefix(subjects[1], "Lembrete: ") || !strings.HasPrefix(subjects[2], "Último aviso: ") || subjects[0] != outOfFundsDraft().EmailCopy.Subject {
		t.Fatalf("each step has its own subject: %v", subjects)
	}
	if notice, _ := rig.emails.sent[2].Placeholders[notification.EpisodeNoticePlaceholder].(string); !strings.Contains(notice, "não vamos enviar mais emails") {
		t.Fatalf("last notice %q", notice)
	}
}

func TestResolvingClosesTheEpisodeAndTheNextOccurrenceStartsAtTheFirstNotice(t *testing.T) {
	rig := newEpisodeRig(t)
	rig.publish(t, outOfFundsDraft())
	rig.clock.advance(day)
	rig.publish(t, outOfFundsDraft())
	if err := rig.publisher.Resolve(context.Background(), "ws-1", "meta_ads_funds:acc-1:funds_out", "meta_ads_funds:acc-1:funds_low"); err != nil {
		t.Fatal(err)
	}
	if first := rig.episodes.all()[0]; first.ClosedAt == nil || first.CloseReason != notification.EpisodeResolved {
		t.Fatalf("episode %+v", first)
	}
	rig.clock.advance(time.Hour)
	rec, _ := rig.publisher.Publish(context.Background(), outOfFundsDraft())
	if rec.Title != outOfFundsDraft().Title {
		t.Fatalf("a new occurrence starts with the first notice, got %q", rec.Title)
	}
	if len(rig.episodes.all()) != 2 || len(rig.store.liveRecords()) != 2 {
		t.Fatal("the closed episode keeps its last entry; the new one adds its own")
	}
}

func TestAnEpisodeWithoutAPublishForTwoDaysClosesSoAForgottenResolveNeverSilencesTheNextOccurrence(t *testing.T) {
	rig := newEpisodeRig(t)
	rig.publish(t, outOfFundsDraft())
	rig.clock.advance(notification.EpisodeQuietClose)
	rec, _ := rig.publisher.Publish(context.Background(), outOfFundsDraft())
	if rec.Title != outOfFundsDraft().Title {
		t.Fatalf("after two quiet days the situation is new, got %q", rec.Title)
	}
	episodes := rig.episodes.all()
	if len(episodes) != 2 || episodes[0].CloseReason != notification.EpisodeWentQuiet || episodes[1].StepsSent != notification.FirstNotice {
		t.Fatalf("episodes %+v", episodes)
	}
}

func TestAMoreSeriousSituationIsItsOwnEpisodeAndNotifiesAtOnce(t *testing.T) {
	rig := newEpisodeRig(t)
	low := outOfFundsDraft()
	low.Severity, low.Title, low.EpisodeKey = notification.SeverityWarning, "Fundos da conta de anúncios acabando", "meta_ads_funds:acc-1:funds_low"
	rig.publish(t, low)
	rig.clock.advance(time.Hour)
	rig.publish(t, outOfFundsDraft())
	if len(rig.queue.fanouts) != 2 || len(rig.episodes.all()) != 2 {
		t.Fatalf("fanouts %v episodes %d", rig.queue.fanouts, len(rig.episodes.all()))
	}
}

func TestAStepIsNeverSentTwiceWhenRecordingItFailsAndThePublishIsRetried(t *testing.T) {
	rig := newEpisodeRig(t)
	rig.episodes.advanceErr = errBoom
	if _, err := rig.publisher.Publish(context.Background(), outOfFundsDraft()); err == nil {
		t.Fatal("a step that could not be recorded is reported so the producer retries")
	}
	rig.episodes.advanceErr = nil
	rig.clock.advance(10 * time.Minute)
	rig.publish(t, outOfFundsDraft())
	if len(rig.store.liveRecords()) != 1 || len(rig.queue.fanouts) != 1 {
		t.Fatalf("the retry finds the step already stored: records %d fanouts %v", len(rig.store.liveRecords()), rig.queue.fanouts)
	}
	if rig.episodes.all()[0].StepsSent != notification.FirstNotice {
		t.Fatal("the retry records the step")
	}
}

func TestAReplicaThatLosesTheStepRaceLeavesTheBellEntryToTheWinner(t *testing.T) {
	rig := newEpisodeRig(t)
	rig.publish(t, outOfFundsDraft())
	first := rig.store.liveRecords()[0]
	rig.clock.advance(day)
	rig.episodes.lostRace = true
	rig.publish(t, outOfFundsDraft())
	if _, err := rig.store.Get(context.Background(), first.ID); err != nil {
		t.Fatal("only the replica that recorded the step withdraws the previous entry")
	}
}

func TestResolveRefusesAMissingWorkspaceAndIgnoresBlankKeys(t *testing.T) {
	rig := newEpisodeRig(t)
	if err := rig.publisher.Resolve(context.Background(), " ", "k"); !errors.Is(err, notification.ErrWorkspaceRequired) {
		t.Fatalf("err = %v", err)
	}
	if err := rig.publisher.Resolve(context.Background(), "ws-1", " ", ""); err != nil {
		t.Fatalf("no keys is nothing to resolve: %v", err)
	}
}

func TestOutOfFundsForTenDaysThenFundedThenOutAgain(t *testing.T) {
	rig := newEpisodeRig(t)
	watch := func(span time.Duration) {
		for end := rig.clock.at.Add(span); rig.clock.at.Before(end); rig.clock.advance(10 * time.Minute) {
			rig.publish(t, outOfFundsDraft())
			rig.deliverQueued(t)
		}
	}
	watch(10 * day)
	if err := rig.publisher.Resolve(context.Background(), "ws-1", "meta_ads_funds:acc-1:funds_out"); err != nil {
		t.Fatal(err)
	}
	rig.clock.advance(3 * day)
	watch(time.Hour)

	subjects := rig.subjects()
	want := []string{"Os anúncios pararam", "Lembrete: ", "Último aviso: ", "Os anúncios pararam"}
	if len(subjects) != len(want) {
		t.Fatalf("emails %v", subjects)
	}
	for i, prefix := range want {
		if !strings.HasPrefix(subjects[i], prefix) {
			t.Errorf("email %d = %q, want it to start with %q", i+1, subjects[i], prefix)
		}
	}
}

func TestRetentionAlsoPurgesOldEpisodes(t *testing.T) {
	store, episodes, clock := newMemoryCenter(), newMemoryEpisodes(), newClock()
	old := clock.at.Add(-RetentionPeriod - time.Hour)
	_, _ = episodes.StartEpisode(context.Background(), notification.NewEpisode("ws-1", "k-old", old))
	_, _ = episodes.StartEpisode(context.Background(), notification.NewEpisode("ws-1", "k-new", clock.at))
	if err := NewRetention(store, episodes, 10, clock.now).Purge(context.Background()); err != nil {
		t.Fatal(err)
	}
	if left := episodes.all(); len(left) != 1 || left[0].Key != "k-new" {
		t.Fatalf("left %+v", left)
	}
}
