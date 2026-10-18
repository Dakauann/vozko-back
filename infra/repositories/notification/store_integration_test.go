package notification_repository

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/notification"
	"vozko/domain/shared"
	"vozko/domain/workspace"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func notificationDB(t *testing.T) *gorm.DB {
	t.Helper()
	return repotest.IsolatedDB(t, "notifications",
		&schema.User{}, &schema.Workspace{}, &schema.WorkspaceCustomRole{}, &schema.WorkspaceMember{},
		&schema.Notification{}, &schema.NotificationRecipient{}, &schema.NotificationPreference{}, &schema.NotificationBroadcast{},
	)
}

var base = time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)

func record(workspaceID string, category notification.Category, at time.Time) notification.Record {
	return notification.Record{
		Draft: notification.Draft{
			WorkspaceID: workspaceID, Category: category, Severity: notification.SeverityWarning,
			Title: "Título " + string(category), Body: "Corpo", Audience: notification.Holding(workspace.ResourceWhatsAppCampaigns, workspace.ActionUpdate),
			ActionURL: "/dashboard", ActionLabel: "Abrir", Email: true, Source: "test",
			EmailCopy: &notification.EmailCopy{Template: "x.html", Subject: "S", Placeholders: map[string]interface{}{"Wave": "3"}},
		},
		Status: notification.StatusPending, CreatedAt: at,
	}
}

func TestStoreCreateGetAndDedupConflict(t *testing.T) {
	store := NewStore(notificationDB(t))
	ctx := context.Background()
	ws := uuid.NewString()

	in := record(ws, notification.CategoryCampaigns, base)
	in.DedupKey = "hold:c-1@1"
	created, isNew, err := store.Create(ctx, in)
	if err != nil || !isNew || created.ID == "" {
		t.Fatalf("create = %+v, %v, %v", created, isNew, err)
	}
	again, isNew, err := store.Create(ctx, in)
	if err != nil || isNew || again.ID != created.ID {
		t.Fatalf("dedup conflict must return the first record: %+v, %v, %v", again, isNew, err)
	}
	other := in
	other.WorkspaceID = uuid.NewString()
	if _, isNew, _ := store.Create(ctx, other); !isNew {
		t.Fatal("the same key in another workspace is a different notification")
	}
	noKey := record(ws, notification.CategoryCampaigns, base)
	first, _, _ := store.Create(ctx, noKey)
	second, isNew, _ := store.Create(ctx, noKey)
	if !isNew || first.ID == second.ID {
		t.Fatal("records without a dedup key never collide")
	}

	got, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Audience.Kind != notification.AudiencePermission || got.Audience.Permission.Resource != workspace.ResourceWhatsAppCampaigns ||
		got.EmailCopy == nil || got.EmailCopy.Placeholders["Wave"] != "3" || !got.Email || got.DedupKey != "hold:c-1@1" || !got.CreatedAt.Equal(base) {
		t.Fatalf("round trip = %+v", got)
	}
	if _, err := store.Get(ctx, uuid.NewString()); !errors.Is(err, notification.ErrNotFound) {
		t.Fatalf("missing = %v", err)
	}
	if _, err := store.Get(ctx, "not-a-uuid"); !errors.Is(err, notification.ErrNotFound) {
		t.Fatalf("malformed id must be not found, got %v", err)
	}
}

func deliver(t *testing.T, store *Store, rec notification.Record, users ...string) {
	t.Helper()
	rs := make([]notification.Recipient, 0, len(users))
	for _, u := range users {
		rs = append(rs, notification.Recipient{NotificationID: rec.ID, WorkspaceID: rec.WorkspaceID, UserID: u, Category: rec.Category, CreatedAt: rec.CreatedAt})
	}
	if err := store.AddRecipients(context.Background(), rs); err != nil {
		t.Fatalf("add recipients: %v", err)
	}
}

func TestInboxPagesFiltersCountsAndMarksOnlyTheViewersRows(t *testing.T) {
	store := NewStore(notificationDB(t))
	ctx := context.Background()
	ws, me, other := uuid.NewString(), uuid.NewString(), uuid.NewString()

	var ids []string
	for i, c := range []notification.Category{notification.CategoryCampaigns, notification.CategoryBilling, notification.CategoryCampaigns, notification.CategoryCampaigns} {
		rec, _, err := store.Create(ctx, record(ws, c, base.Add(time.Duration(i)*time.Minute)))
		if err != nil {
			t.Fatal(err)
		}
		deliver(t, store, rec, me, other)
		deliver(t, store, rec, me)
		ids = append(ids, rec.ID)
	}
	viewer := notification.Viewer{WorkspaceID: ws, UserID: me}

	page1, err := store.List(ctx, viewer, notification.ListQuery{Limit: 2})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page1) != 2 || page1[0].ID != ids[3] || page1[1].ID != ids[2] || page1[0].Title == "" {
		t.Fatalf("newest first = %+v", page1)
	}
	cursor := shared.Keyset{At: page1[1].CreatedAt, ID: page1[1].ID}
	page2, _ := store.List(ctx, viewer, notification.ListQuery{Limit: 5, Before: cursor, Paged: true})
	if len(page2) != 2 || page2[0].ID != ids[1] || page2[1].ID != ids[0] {
		t.Fatalf("second page = %+v", page2)
	}
	onlyCampaigns, _ := store.List(ctx, viewer, notification.ListQuery{Limit: 10, Category: notification.CategoryCampaigns})
	if len(onlyCampaigns) != 3 {
		t.Fatalf("campaigns = %d", len(onlyCampaigns))
	}

	summary, err := store.Summary(ctx, viewer, 1000)
	if err != nil || summary.Unread != 4 || summary.ByCategory[notification.CategoryCampaigns] != 3 || summary.Capped {
		t.Fatalf("summary = %+v, %v", summary, err)
	}
	capped, _ := store.Summary(ctx, viewer, 3)
	if capped.Unread != 3 || !capped.Capped {
		t.Fatalf("capped summary = %+v", capped)
	}

	if n, err := store.MarkRead(ctx, viewer, []string{ids[0], ids[1], "not-a-uuid"}, base.Add(time.Hour)); err != nil || n != 2 {
		t.Fatalf("mark read = %d, %v", n, err)
	}
	if n, _ := store.MarkRead(ctx, viewer, []string{ids[0]}, base.Add(2*time.Hour)); n != 0 {
		t.Fatalf("marking a read row again changes nothing, got %d", n)
	}
	unread, _ := store.List(ctx, viewer, notification.ListQuery{Limit: 10, UnreadOnly: true})
	if len(unread) != 2 {
		t.Fatalf("unread = %d", len(unread))
	}
	if n, _ := store.MarkAllRead(ctx, viewer, notification.CategoryCampaigns, base.Add(time.Hour)); n != 2 {
		t.Fatalf("mark all campaigns = %d", n)
	}
	if n, _ := store.MarkUnread(ctx, viewer, []string{ids[1]}); n != 1 {
		t.Fatalf("mark unread = %d", n)
	}
	s, _ := store.Summary(ctx, viewer, 1000)
	if s.Unread != 1 || s.ByCategory[notification.CategoryBilling] != 1 {
		t.Fatalf("after marks = %+v", s)
	}
	otherSummary, _ := store.Summary(ctx, notification.Viewer{WorkspaceID: ws, UserID: other}, 1000)
	if otherSummary.Unread != 4 {
		t.Fatalf("another person's read state is untouched, got %+v", otherSummary)
	}
	strangerView, _ := store.List(ctx, notification.Viewer{WorkspaceID: uuid.NewString(), UserID: me}, notification.ListQuery{Limit: 10})
	if len(strangerView) != 0 {
		t.Fatal("the same person in another workspace sees nothing")
	}
}

func TestDeliveryLifecycleAndPurge(t *testing.T) {
	store := NewStore(notificationDB(t))
	ctx := context.Background()
	ws, me := uuid.NewString(), uuid.NewString()

	old, _, _ := store.Create(ctx, record(ws, notification.CategorySystem, base.Add(-200*24*time.Hour)))
	fresh, _, _ := store.Create(ctx, record(ws, notification.CategorySystem, base))
	deliver(t, store, old, me)
	deliver(t, store, fresh, me)

	stale, err := store.StalePending(ctx, base.Add(-time.Minute), 10)
	if err != nil || !reflect.DeepEqual(stale, []string{old.ID}) {
		t.Fatalf("stale = %v, %v", stale, err)
	}
	if err := store.MarkDelivered(ctx, old.ID, 1, base); err != nil {
		t.Fatal(err)
	}
	if stale, _ := store.StalePending(ctx, base.Add(time.Minute), 10); !reflect.DeepEqual(stale, []string{fresh.ID}) {
		t.Fatalf("delivered records are not stale: %v", stale)
	}
	got, _ := store.Get(ctx, old.ID)
	if got.Status != notification.StatusDelivered || got.RecipientCount != 1 || got.DeliveredAt == nil {
		t.Fatalf("delivered = %+v", got)
	}

	purged, err := store.PurgeBefore(ctx, base.Add(-180*24*time.Hour), 100)
	if err != nil || purged != 1 {
		t.Fatalf("purge = %d, %v", purged, err)
	}
	if _, err := store.Get(ctx, old.ID); !errors.Is(err, notification.ErrNotFound) {
		t.Fatal("purged record must be gone")
	}
	items, _ := store.List(ctx, notification.Viewer{WorkspaceID: ws, UserID: me}, notification.ListQuery{Limit: 10})
	if len(items) != 1 || items[0].ID != fresh.ID {
		t.Fatalf("purge removes recipient rows with their record: %+v", items)
	}
}

func TestAddRecipientsInChunksIsIdempotent(t *testing.T) {
	store := NewStore(notificationDB(t))
	ctx := context.Background()
	ws := uuid.NewString()
	rec, _, _ := store.Create(ctx, record(ws, notification.CategoryTeam, base))
	users := make([]string, recipientBatch*2+7)
	for i := range users {
		users[i] = uuid.NewString()
	}
	deliver(t, store, rec, users...)
	deliver(t, store, rec, users...)
	var count int64
	store.db.Model(&schema.NotificationRecipient{}).Where("notification_id = ?", rec.ID).Count(&count)
	if int(count) != len(users) {
		t.Fatalf("rows = %d, want %d", count, len(users))
	}
}

func TestPreferencesRoundTripAndBatchLookup(t *testing.T) {
	store := NewPreferenceStore(notificationDB(t))
	ctx := context.Background()
	ws, a, b := uuid.NewString(), uuid.NewString(), uuid.NewString()

	empty, err := store.Preferences(ctx, notification.Viewer{WorkspaceID: ws, UserID: a})
	if err != nil || len(empty.EmailMuted) != 0 {
		t.Fatalf("default = %+v, %v", empty, err)
	}
	for _, muted := range [][]notification.Category{{notification.CategoryBilling}, {notification.CategoryCampaigns, notification.CategoryTeam}} {
		if _, err := store.SavePreferences(ctx, notification.Preferences{WorkspaceID: ws, UserID: a, EmailMuted: muted, UpdatedAt: base}); err != nil {
			t.Fatalf("save: %v", err)
		}
	}
	got, _ := store.Preferences(ctx, notification.Viewer{WorkspaceID: ws, UserID: a})
	if !reflect.DeepEqual(got.EmailMuted, []notification.Category{notification.CategoryCampaigns, notification.CategoryTeam}) {
		t.Fatalf("saved twice = %v", got.EmailMuted)
	}
	if _, err := store.SavePreferences(ctx, notification.Preferences{WorkspaceID: ws, UserID: b, EmailDigest: true, UpdatedAt: base}); err != nil {
		t.Fatal(err)
	}
	prefs, err := store.PreferencesFor(ctx, ws, []string{a, b, uuid.NewString()})
	if err != nil || len(prefs) != 2 || len(prefs[a].EmailMuted) != 2 || prefs[a].EmailDigest || !prefs[b].EmailDigest {
		t.Fatalf("preferences for = %+v, %v", prefs, err)
	}
	if none, err := store.PreferencesFor(ctx, ws, nil); err != nil || len(none) != 0 {
		t.Fatalf("no ids = %v, %v", none, err)
	}
}

func TestBroadcastStoreLifecycle(t *testing.T) {
	store := NewBroadcastStore(notificationDB(t))
	ctx := context.Background()
	admin := uuid.NewString()

	var created []notification.Broadcast
	for i := 0; i < 3; i++ {
		b, err := store.CreateBroadcast(ctx, notification.Broadcast{
			BroadcastDraft: notification.BroadcastDraft{
				Target: notification.BroadcastTarget{WorkspaceIDs: []string{uuid.NewString()}}, Audience: notification.Users("u-1"),
				Category: notification.CategoryAnnouncements, Severity: notification.SeverityInfo, Title: fmt.Sprintf("B%d", i), Email: true,
			},
			CreatedBy: admin, Status: notification.BroadcastQueued, CreatedAt: base.Add(time.Duration(i) * time.Minute),
		})
		if err != nil || b.ID == "" {
			t.Fatalf("create = %+v, %v", b, err)
		}
		created = append(created, b)
	}
	got, err := store.Broadcast(ctx, created[0].ID)
	if err != nil || got.Title != "B0" || len(got.Target.WorkspaceIDs) != 1 || got.Audience.UserIDs[0] != "u-1" || got.CreatedBy != admin {
		t.Fatalf("get = %+v, %v", got, err)
	}
	if err := store.CompleteBroadcast(ctx, created[0].ID, 1, base.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	queued, _ := store.StaleQueued(ctx, base.Add(10*time.Minute), 10)
	sort.Strings(queued)
	want := []string{created[1].ID, created[2].ID}
	sort.Strings(want)
	if !reflect.DeepEqual(queued, want) {
		t.Fatalf("stale queued = %v", queued)
	}
	page, _ := store.Broadcasts(ctx, shared.Keyset{}, false, 2)
	if len(page) != 2 || page[0].ID != created[2].ID {
		t.Fatalf("newest first = %+v", page)
	}
	next, _ := store.Broadcasts(ctx, shared.Keyset{At: page[1].CreatedAt, ID: page[1].ID}, true, 2)
	if len(next) != 1 || next[0].ID != created[0].ID || next[0].Status != notification.BroadcastCompleted || next[0].WorkspaceCount != 1 {
		t.Fatalf("second page = %+v", next)
	}
	if _, err := store.Broadcast(ctx, uuid.NewString()); !errors.Is(err, notification.ErrNotFound) {
		t.Fatalf("missing = %v", err)
	}
}

func TestDirectoryListsActiveMembersAndLiveWorkspaces(t *testing.T) {
	db := notificationDB(t)
	directory := NewDirectory(db)
	ctx := context.Background()

	owner := schema.User{Username: "ana", Email: "ana@x.com", Password: "x"}
	agent := schema.User{Username: "beto", Email: "beto@x.com", Password: "x"}
	gone := schema.User{Username: "caio", Email: "caio@x.com", Password: "x", DisabledAt: &base}
	for _, u := range []*schema.User{&owner, &agent, &gone} {
		if err := db.Create(u).Error; err != nil {
			t.Fatal(err)
		}
	}
	ws := schema.Workspace{OwnerID: owner.ID, Name: "Loja Centro"}
	deleted := schema.Workspace{OwnerID: owner.ID, Name: "Antiga", IsDefault: true}
	for _, w := range []*schema.Workspace{&ws, &deleted} {
		if err := db.Create(w).Error; err != nil {
			t.Fatal(err)
		}
	}
	db.Delete(&deleted)
	for _, m := range []schema.WorkspaceMember{
		{WorkspaceID: ws.ID, UserID: owner.ID, Role: "owner"},
		{WorkspaceID: ws.ID, UserID: agent.ID, Role: "member"},
		{WorkspaceID: ws.ID, UserID: gone.ID, Role: "admin"},
	} {
		if err := db.Create(&m).Error; err != nil {
			t.Fatal(err)
		}
	}

	members, err := directory.Members(ctx, ws.ID)
	if err != nil {
		t.Fatalf("members: %v", err)
	}
	sort.Slice(members, func(i, j int) bool { return members[i].Email < members[j].Email })
	if len(members) != 2 || members[0].Email != "ana@x.com" || !members[0].Manager || members[0].Name != "ana" || members[1].Manager {
		t.Fatalf("members = %+v", members)
	}
	name, err := directory.WorkspaceName(ctx, ws.ID)
	if err != nil || name != "Loja Centro" {
		t.Fatalf("name = %q, %v", name, err)
	}
	ids, err := directory.WorkspaceIDs(ctx, "", 10)
	if err != nil || !reflect.DeepEqual(ids, []string{ws.ID}) {
		t.Fatalf("live workspaces = %v, %v", ids, err)
	}
	if after, _ := directory.WorkspaceIDs(ctx, ws.ID, 10); len(after) != 0 {
		t.Fatalf("after the last = %v", after)
	}
}

func TestDigestRowsAreHeldGroupedPerPersonAndReleased(t *testing.T) {
	store := NewStore(notificationDB(t))
	ctx := context.Background()
	ws, me, other := uuid.NewString(), uuid.NewString(), uuid.NewString()
	cutoff := base.Add(time.Hour)

	var held []notification.Record
	for i := 0; i < 3; i++ {
		rec, _, _ := store.Create(ctx, record(ws, notification.CategoryCampaigns, base.Add(time.Duration(i)*time.Minute)))
		deliver(t, store, rec, me, other)
		if err := store.HoldForDigest(ctx, rec.ID, []string{me, other}); err != nil {
			t.Fatalf("hold: %v", err)
		}
		held = append(held, rec)
	}
	late, _, _ := store.Create(ctx, record(ws, notification.CategoryBilling, cutoff.Add(time.Minute)))
	deliver(t, store, late, me)
	_ = store.HoldForDigest(ctx, late.ID, []string{me})
	_, _ = store.MarkRead(ctx, notification.Viewer{WorkspaceID: ws, UserID: other}, []string{held[0].ID, held[1].ID, held[2].ID}, base)

	workspaces, err := store.WorkspacesWithHeldDigests(ctx, cutoff, "", 10)
	if err != nil || !reflect.DeepEqual(workspaces, []string{ws}) {
		t.Fatalf("workspaces = %v, %v", workspaces, err)
	}
	if after, _ := store.WorkspacesWithHeldDigests(ctx, cutoff, ws, 10); len(after) != 0 {
		t.Fatalf("paging past the last workspace = %v", after)
	}

	digests, err := store.HeldDigests(ctx, ws, cutoff, 2)
	if err != nil || len(digests) != 1 {
		t.Fatalf("digests = %+v, %v; the person who read everything has none", digests, err)
	}
	d := digests[0]
	if d.UserID != me || d.Total != 3 || len(d.Items) != 2 || d.Items[0].ID != held[2].ID || d.Items[0].Title == "" {
		t.Fatalf("digest = %+v, want the two newest of three held before the cutoff", d)
	}

	if err := store.ReleaseDigests(ctx, ws, cutoff, []string{me}); err != nil {
		t.Fatalf("release: %v", err)
	}
	if again, _ := store.HeldDigests(ctx, ws, cutoff, 2); len(again) != 1 {
		t.Fatal("a kept person stays held")
	}
	if err := store.ReleaseDigests(ctx, ws, cutoff, nil); err != nil {
		t.Fatal(err)
	}
	if left, _ := store.WorkspacesWithHeldDigests(ctx, cutoff, "", 10); len(left) != 0 {
		t.Fatalf("everything before the cutoff is released, got %v", left)
	}
	if later, _ := store.HeldDigests(ctx, ws, cutoff.Add(24*time.Hour), 20); len(later) != 1 || later[0].Total != 1 {
		t.Fatalf("the item held after the cutoff waits for the next digest: %+v", later)
	}
}

func TestBroadcastReadStatsAndRecipientsJoinPeopleAndWorkspaces(t *testing.T) {
	db := notificationDB(t)
	store, broadcasts, directory := NewStore(db), NewBroadcastStore(db), NewDirectory(db)
	ctx := context.Background()

	ana := schema.User{Username: "ana", Email: "ana@x.com", Password: "x"}
	beto := schema.User{Username: "beto", Email: "beto@x.com", Password: "x"}
	for _, u := range []*schema.User{&ana, &beto} {
		if err := db.Create(u).Error; err != nil {
			t.Fatal(err)
		}
	}
	lojaA := schema.Workspace{OwnerID: ana.ID, Name: "Loja A"}
	lojaB := schema.Workspace{OwnerID: ana.ID, Name: "Loja B", IsDefault: true}
	for _, w := range []*schema.Workspace{&lojaA, &lojaB} {
		if err := db.Create(w).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, m := range []schema.WorkspaceMember{
		{WorkspaceID: lojaA.ID, UserID: ana.ID, Role: "owner"}, {WorkspaceID: lojaA.ID, UserID: beto.ID, Role: "member"},
		{WorkspaceID: lojaB.ID, UserID: ana.ID, Role: "owner"},
	} {
		if err := db.Create(&m).Error; err != nil {
			t.Fatal(err)
		}
	}

	members, err := directory.MembersOf(ctx, []string{lojaA.ID, lojaB.ID, uuid.NewString(), "bad"})
	if err != nil || len(members[lojaA.ID]) != 2 || len(members[lojaB.ID]) != 1 || len(members) != 2 {
		t.Fatalf("members of = %+v, %v", members, err)
	}

	b, _ := broadcasts.CreateBroadcast(ctx, notification.Broadcast{
		BroadcastDraft: notification.BroadcastDraft{Target: notification.BroadcastTarget{All: true}, Audience: notification.Everyone(), Category: notification.CategoryAnnouncements, Severity: notification.SeverityInfo, Title: "Novidade"},
		CreatedBy: ana.ID, Status: notification.BroadcastQueued, CreatedAt: base,
	})
	other, _ := broadcasts.CreateBroadcast(ctx, notification.Broadcast{
		BroadcastDraft: notification.BroadcastDraft{Target: notification.BroadcastTarget{All: true}, Audience: notification.Everyone(), Category: notification.CategoryAnnouncements, Severity: notification.SeverityInfo, Title: "Outra"},
		CreatedBy: ana.ID, Status: notification.BroadcastQueued, CreatedAt: base,
	})
	for _, ws := range []schema.Workspace{lojaA, lojaB} {
		r := record(ws.ID, notification.CategoryAnnouncements, base)
		r.BroadcastID, r.DedupKey = b.ID, "broadcast:"+b.ID
		rec, _, err := store.Create(ctx, r)
		if err != nil {
			t.Fatal(err)
		}
		if ws.ID == lojaA.ID {
			deliver(t, store, rec, ana.ID, beto.ID)
			_, _ = store.MarkRead(ctx, notification.Viewer{WorkspaceID: ws.ID, UserID: beto.ID}, []string{rec.ID}, base.Add(time.Hour))
		} else {
			deliver(t, store, rec, ana.ID)
		}
	}

	stats, err := broadcasts.BroadcastStats(ctx, []string{b.ID, other.ID, "bad"})
	if err != nil || stats[b.ID] != (notification.BroadcastStats{Recipients: 3, Read: 1}) || stats[other.ID] != (notification.BroadcastStats{}) {
		t.Fatalf("stats = %+v, %v", stats, err)
	}

	readers, err := broadcasts.BroadcastRecipients(ctx, b.ID, notification.RecipientQuery{Status: notification.ReadStatusRead, Limit: 10})
	if err != nil || len(readers) != 1 || readers[0].Name != "beto" || readers[0].Email != "beto@x.com" || readers[0].WorkspaceName != "Loja A" || readers[0].ReadAt == nil {
		t.Fatalf("readers = %+v, %v", readers, err)
	}
	unread, _ := broadcasts.BroadcastRecipients(ctx, b.ID, notification.RecipientQuery{Status: notification.ReadStatusUnread, Limit: 10})
	if len(unread) != 2 {
		t.Fatalf("unread = %+v", unread)
	}
	first, _ := broadcasts.BroadcastRecipients(ctx, b.ID, notification.RecipientQuery{Status: notification.ReadStatusAll, Limit: 2})
	last := first[len(first)-1]
	rest, _ := broadcasts.BroadcastRecipients(ctx, b.ID, notification.RecipientQuery{
		Status: notification.ReadStatusAll, Paged: true, Limit: 10,
		After: notification.RecipientCursor{NotificationID: last.NotificationID, UserID: last.UserID},
	})
	if len(first) != 2 || len(rest) != 1 {
		t.Fatalf("pages = %d + %d", len(first), len(rest))
	}
	if _, err := broadcasts.BroadcastRecipients(ctx, b.ID, notification.RecipientQuery{Paged: true, After: notification.RecipientCursor{NotificationID: "x", UserID: "y"}, Limit: 5}); !errors.Is(err, notification.ErrCursorInvalid) {
		t.Fatalf("foreign cursor = %v", err)
	}
}
