package notification_repository

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/notification"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func episodeDB(t *testing.T) *gorm.DB {
	t.Helper()
	return repotest.IsolatedDB(t, "notification_episodes",
		&schema.Notification{}, &schema.NotificationRecipient{}, &schema.NotificationEpisode{},
	)
}

func TestAnOpenEpisodeIsUniquePerWorkspaceAndConditionEvenUnderConcurrentStarts(t *testing.T) {
	episodes := NewEpisodeStore(episodeDB(t))
	ctx := context.Background()
	ws := uuid.NewString()

	var wg sync.WaitGroup
	ids := make([]string, 8)
	errs := make([]error, 8)
	for i := range ids {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			e, err := episodes.StartEpisode(ctx, notification.NewEpisode(ws, "meta_ads_funds:acc-1:funds_out", base))
			ids[i], errs[i] = e.ID, err
		}(i)
	}
	wg.Wait()
	for i := range ids {
		if errs[i] != nil || ids[i] != ids[0] {
			t.Fatalf("replicas starting the same episode must share it: %v %v", ids, errs)
		}
	}
	other, err := episodes.StartEpisode(ctx, notification.NewEpisode(uuid.NewString(), "meta_ads_funds:acc-1:funds_out", base))
	if err != nil || other.ID == ids[0] {
		t.Fatalf("another workspace has its own episode: %+v %v", other, err)
	}

	open, found, err := episodes.OpenEpisode(ctx, ws, "meta_ads_funds:acc-1:funds_out")
	if err != nil || !found || open.ID != ids[0] || open.StepsSent != 0 || !open.OpenedAt.Equal(base) || !open.LastSeenAt.Equal(base) {
		t.Fatalf("open = %+v %v %v", open, found, err)
	}
	if _, found, _ := episodes.OpenEpisode(ctx, ws, "meta_ads_funds:acc-1:funds_low"); found {
		t.Fatal("another condition is another episode")
	}
}

func TestAStepIsRecordedOnceEvenWhenTwoReplicasTry(t *testing.T) {
	db := episodeDB(t)
	episodes := NewEpisodeStore(db)
	ctx := context.Background()
	ws := uuid.NewString()
	e, _ := episodes.StartEpisode(ctx, notification.NewEpisode(ws, "k", base))
	first, second := uuid.NewString(), uuid.NewString()

	won, err := episodes.AdvanceEpisode(ctx, e.ID, notification.FirstNotice, first, base)
	if err != nil || !won {
		t.Fatalf("advance = %v %v", won, err)
	}
	won, err = episodes.AdvanceEpisode(ctx, e.ID, notification.FirstNotice, second, base)
	if err != nil || won {
		t.Fatalf("the same step must not be recorded twice: %v %v", won, err)
	}
	if won, _ := episodes.AdvanceEpisode(ctx, e.ID, notification.LastNotice, second, base); won {
		t.Fatal("a step cannot be skipped")
	}
	later := base.Add(24 * time.Hour)
	if won, _ := episodes.AdvanceEpisode(ctx, e.ID, notification.Reminder, second, later); !won {
		t.Fatal("the next step advances")
	}
	open, _, _ := episodes.OpenEpisode(ctx, ws, "k")
	if open.StepsSent != notification.Reminder || open.NotificationID != second || open.LastSentAt == nil || !open.LastSentAt.Equal(later) {
		t.Fatalf("open = %+v", open)
	}
}

func TestSeeingAnEpisodeNeverMovesItsLastSightingBack(t *testing.T) {
	episodes := NewEpisodeStore(episodeDB(t))
	ctx := context.Background()
	ws := uuid.NewString()
	e, _ := episodes.StartEpisode(ctx, notification.NewEpisode(ws, "k", base))
	later := base.Add(time.Hour)
	if err := episodes.SeeEpisode(ctx, e.ID, later); err != nil {
		t.Fatal(err)
	}
	if err := episodes.SeeEpisode(ctx, e.ID, base.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	open, _, _ := episodes.OpenEpisode(ctx, ws, "k")
	if !open.LastSeenAt.Equal(later) {
		t.Fatalf("last seen = %v, want %v", open.LastSeenAt, later)
	}
}

func TestResolvingAndClosingEndTheEpisodeSoTheNextStartOpensANewOne(t *testing.T) {
	episodes := NewEpisodeStore(episodeDB(t))
	ctx := context.Background()
	ws := uuid.NewString()
	out, _ := episodes.StartEpisode(ctx, notification.NewEpisode(ws, "funds_out", base))
	low, _ := episodes.StartEpisode(ctx, notification.NewEpisode(ws, "funds_low", base))
	untouched, _ := episodes.StartEpisode(ctx, notification.NewEpisode(ws, "phone_alert:banned:p-1", base))

	resolvedAt := base.Add(10 * 24 * time.Hour)
	n, err := episodes.ResolveEpisodes(ctx, ws, []string{"funds_out", "funds_low", "never_opened"}, resolvedAt)
	if err != nil || n != 2 {
		t.Fatalf("resolve = %d %v", n, err)
	}
	if again, _ := episodes.ResolveEpisodes(ctx, ws, []string{"funds_out"}, resolvedAt); again != 0 {
		t.Fatal("resolving twice changes nothing")
	}
	if _, found, _ := episodes.OpenEpisode(ctx, ws, "funds_out"); found {
		t.Fatal("a resolved episode is not open")
	}
	if _, found, _ := episodes.OpenEpisode(ctx, ws, "phone_alert:banned:p-1"); !found {
		t.Fatal("only the named conditions resolve")
	}
	reopened, _ := episodes.StartEpisode(ctx, notification.NewEpisode(ws, "funds_out", resolvedAt.Add(time.Hour)))
	if reopened.ID == out.ID || reopened.ID == low.ID {
		t.Fatal("a new occurrence after resolving is a new episode")
	}

	if err := episodes.CloseEpisode(ctx, untouched.ID, resolvedAt, notification.EpisodeWentQuiet); err != nil {
		t.Fatal(err)
	}
	var row schema.NotificationEpisode
	episodes.db.Where("id = ?", untouched.ID).Take(&row)
	if row.ClosedAt == nil || row.CloseReason != string(notification.EpisodeWentQuiet) {
		t.Fatalf("closed row %+v", row)
	}
	var resolvedRow schema.NotificationEpisode
	episodes.db.Where("id = ?", out.ID).Take(&resolvedRow)
	if resolvedRow.CloseReason != string(notification.EpisodeResolved) || !resolvedRow.ClosedAt.Equal(resolvedAt) {
		t.Fatalf("resolved row %+v", resolvedRow)
	}
}

func TestEpisodesOfAnotherWorkspaceAreNeverResolved(t *testing.T) {
	episodes := NewEpisodeStore(episodeDB(t))
	ctx := context.Background()
	mine, theirs := uuid.NewString(), uuid.NewString()
	_, _ = episodes.StartEpisode(ctx, notification.NewEpisode(theirs, "funds_out", base))
	if n, err := episodes.ResolveEpisodes(ctx, mine, []string{"funds_out"}, base); err != nil || n != 0 {
		t.Fatalf("resolve = %d %v", n, err)
	}
	if _, err := episodes.ResolveEpisodes(ctx, "not-a-uuid", []string{"funds_out"}, base); !errors.Is(err, notification.ErrWorkspaceRequired) {
		t.Fatalf("a malformed workspace is refused before SQL: %v", err)
	}
}

func TestOldEpisodesArePurgedAndRecentOnesKept(t *testing.T) {
	episodes := NewEpisodeStore(episodeDB(t))
	ctx := context.Background()
	ws := uuid.NewString()
	old, _ := episodes.StartEpisode(ctx, notification.NewEpisode(ws, "old-open", base.Add(-200*24*time.Hour)))
	closed, _ := episodes.StartEpisode(ctx, notification.NewEpisode(ws, "old-closed", base.Add(-300*24*time.Hour)))
	_ = episodes.CloseEpisode(ctx, closed.ID, base.Add(-190*24*time.Hour), notification.EpisodeResolved)
	recent, _ := episodes.StartEpisode(ctx, notification.NewEpisode(ws, "recent", base))

	n, err := episodes.PurgeEpisodesBefore(ctx, base.Add(-180*24*time.Hour), 10)
	if err != nil || n != 2 {
		t.Fatalf("purge = %d %v", n, err)
	}
	var left []schema.NotificationEpisode
	episodes.db.Find(&left)
	if len(left) != 1 || left[0].ID != recent.ID || left[0].ID == old.ID {
		t.Fatalf("left %+v", left)
	}
}

func TestWithdrawingAnEntryRemovesItAndItsRecipientRows(t *testing.T) {
	db := episodeDB(t)
	store := NewStore(db)
	ctx := context.Background()
	ws := uuid.NewString()
	rec, _, err := store.Create(ctx, record(ws, notification.CategoryAdvertising, base))
	if err != nil {
		t.Fatal(err)
	}
	user := uuid.NewString()
	if err := store.AddRecipients(ctx, []notification.Recipient{{NotificationID: rec.ID, WorkspaceID: ws, UserID: user, Category: notification.CategoryAdvertising, CreatedAt: base}}); err != nil {
		t.Fatal(err)
	}
	kept, _, _ := store.Create(ctx, record(ws, notification.CategoryAdvertising, base.Add(time.Minute)))

	if err := store.Withdraw(ctx, rec.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, rec.ID); !errors.Is(err, notification.ErrNotFound) {
		t.Fatalf("withdrawn entry still readable: %v", err)
	}
	summary, err := store.Summary(ctx, notification.Viewer{WorkspaceID: ws, UserID: user}, 100)
	if err != nil || summary.Unread != 0 {
		t.Fatalf("the unread count must not keep a withdrawn entry: %+v %v", summary, err)
	}
	if _, err := store.Get(ctx, kept.ID); err != nil {
		t.Fatal("other entries stay")
	}
	if err := store.Withdraw(ctx, "not-a-uuid"); err != nil {
		t.Fatalf("a malformed id withdraws nothing: %v", err)
	}
}
