package notification_usecase

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"

	"vozko/domain/notification"
)

func deliveredInbox(t *testing.T) (*centerRig, *InboxService, notification.Viewer) {
	t.Helper()
	rig := newCenterRig(t, teamDirectory(), nil)
	for _, c := range []notification.Category{notification.CategoryCampaigns, notification.CategoryBilling, notification.CategoryCampaigns} {
		d := campaignDraft()
		d.Category = c
		rec, err := rig.publisher.Publish(context.Background(), d)
		if err != nil {
			t.Fatal(err)
		}
		if err := rig.delivery.Deliver(context.Background(), rec.ID); err != nil {
			t.Fatal(err)
		}
	}
	return rig, NewInboxService(rig.store, rig.store, rig.clock.now), notification.Viewer{WorkspaceID: "ws-1", UserID: "agent"}
}

func TestInboxListPagesNewestFirstWithACursor(t *testing.T) {
	_, inbox, viewer := deliveredInbox(t)
	first, err := inbox.List(context.Background(), viewer, notification.ListQuery{Limit: 2})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(first.Items) != 2 || first.Items[0].ID != "n-3" || first.Next == "" {
		t.Fatalf("first page = %+v", first)
	}
	q, err := notification.ParseListQuery("", false, first.Next, 2)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := inbox.List(context.Background(), viewer, q)
	if len(second.Items) != 1 || second.Items[0].ID != "n-1" || second.Next != "" {
		t.Fatalf("second page = %+v", second)
	}
}

func TestInboxIsPrivateToTheViewerAndTheWorkspace(t *testing.T) {
	_, inbox, _ := deliveredInbox(t)
	other, _ := inbox.List(context.Background(), notification.Viewer{WorkspaceID: "ws-2", UserID: "agent"}, notification.ListQuery{})
	if len(other.Items) != 0 {
		t.Fatalf("another workspace must see nothing, got %d", len(other.Items))
	}
	if _, err := inbox.List(context.Background(), notification.Viewer{WorkspaceID: "ws-1"}, notification.ListQuery{}); !errors.Is(err, notification.ErrViewerRequired) {
		t.Fatalf("no viewer = %v", err)
	}
}

func TestReadStateCountsAndMarkAllByCategory(t *testing.T) {
	_, inbox, viewer := deliveredInbox(t)
	ctx := context.Background()

	s, _ := inbox.Summary(ctx, viewer)
	if s.Unread != 3 || s.ByCategory[notification.CategoryCampaigns] != 2 || s.ByCategory[notification.CategoryBilling] != 1 {
		t.Fatalf("summary = %+v", s)
	}

	if n, err := inbox.MarkRead(ctx, viewer, []string{"n-1", "n-1"}); err != nil || n != 1 {
		t.Fatalf("mark read = %d, %v", n, err)
	}
	if n, _ := inbox.MarkAllRead(ctx, viewer, "campaigns"); n != 1 {
		t.Fatalf("mark all campaigns read = %d, want the one left", n)
	}
	s, _ = inbox.Summary(ctx, viewer)
	if s.Unread != 1 || s.ByCategory[notification.CategoryBilling] != 1 {
		t.Fatalf("summary after = %+v", s)
	}

	if n, _ := inbox.MarkUnread(ctx, viewer, []string{"n-3"}); n != 1 {
		t.Fatalf("mark unread = %d", n)
	}
	unread, _ := inbox.List(ctx, viewer, notification.ListQuery{UnreadOnly: true})
	if len(unread.Items) != 2 {
		t.Fatalf("unread items = %d", len(unread.Items))
	}

	if _, err := inbox.MarkAllRead(ctx, viewer, "misc"); !errors.Is(err, notification.ErrCategoryUnknown) {
		t.Fatalf("unknown category = %v", err)
	}
	if _, err := inbox.MarkRead(ctx, viewer, nil); !errors.Is(err, notification.ErrIDsInvalid) {
		t.Fatalf("no ids = %v", err)
	}
}

func TestPreferencesRoundTripAndListTheCatalog(t *testing.T) {
	_, inbox, viewer := deliveredInbox(t)
	saved, err := inbox.SavePreferences(context.Background(), viewer, PreferencesChange{EmailMuted: []string{"billing", "billing", "campaigns"}, EmailDigest: true})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	want := []notification.Category{notification.CategoryBilling, notification.CategoryCampaigns}
	if !reflect.DeepEqual(saved.EmailMuted, want) {
		t.Fatalf("saved = %v", saved.EmailMuted)
	}
	got, _ := inbox.Preferences(context.Background(), viewer)
	if !reflect.DeepEqual(got.EmailMuted, want) || !got.EmailDigest || !saved.EmailDigest {
		t.Fatalf("read = %+v", got)
	}
	if _, err := inbox.SavePreferences(context.Background(), viewer, PreferencesChange{EmailMuted: []string{"misc"}}); !errors.Is(err, notification.ErrCategoryUnknown) {
		t.Fatalf("unknown = %v", err)
	}
}

func TestBroadcastIsStoredQueuedThenExpandedOncePerWorkspace(t *testing.T) {
	directory := fixedDirectory{
		workspaces: []string{"ws-1", "ws-2", "ws-3"},
		members: map[string][]notification.Member{
			"ws-1": {{UserID: "owner", Email: "owner@x.com", Manager: true}, {UserID: "agent", Email: "agent@x.com"}},
			"ws-2": {{UserID: "owner", Email: "owner@x.com", Manager: true}},
			"ws-3": {{UserID: "solo", Email: "solo@x.com", Manager: true}},
		},
	}
	rig := newCenterRig(t, directory, nil)
	broadcasts, err := NewBroadcasts(BroadcastsDeps{Store: rig.store, Directory: directory, Publisher: rig.publisher, Queue: rig.queue, Images: rig.images, Preferences: rig.store, Now: rig.clock.now, PageSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	b, err := broadcasts.Create(ctx, "admin-1", notification.BroadcastDraft{
		Target: notification.BroadcastTarget{All: true}, Audience: notification.Managers(),
		Category: notification.CategoryAnnouncements, Title: "Novidade", Body: "Chegou.", Email: true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if b.Status != notification.BroadcastQueued || b.CreatedBy != "admin-1" || !reflect.DeepEqual(rig.queue.broadcasts, []string{b.ID}) {
		t.Fatalf("broadcast = %+v queued = %v", b, rig.queue.broadcasts)
	}

	if err := broadcasts.Expand(ctx, b.ID); err != nil {
		t.Fatalf("expand: %v", err)
	}
	if err := broadcasts.Expand(ctx, b.ID); err != nil {
		t.Fatalf("expand again: %v", err)
	}
	if len(rig.store.records) != 3 || len(rig.queue.fanouts) != 3 {
		t.Fatalf("one notification per workspace, got %d records and %d fanouts", len(rig.store.records), len(rig.queue.fanouts))
	}
	for _, id := range rig.queue.fanouts {
		if err := rig.delivery.Deliver(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	if got := rig.emails.emails(); !reflect.DeepEqual(got, []string{"owner@x.com", "solo@x.com"}) {
		t.Fatalf("one email per person per broadcast even across workspaces, got %v", got)
	}
	stored, _ := rig.store.Broadcast(ctx, b.ID)
	if stored.Status != notification.BroadcastCompleted || stored.WorkspaceCount != 3 {
		t.Fatalf("stored = %+v", stored)
	}

	page, err := broadcasts.List(ctx, "", 10)
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("list = %+v, %v", page, err)
	}
}

func TestBroadcastToListedWorkspacesAndRefusals(t *testing.T) {
	rig := newCenterRig(t, teamDirectory(), nil)
	broadcasts, _ := NewBroadcasts(BroadcastsDeps{Store: rig.store, Directory: teamDirectory(), Publisher: rig.publisher, Queue: rig.queue, Images: rig.images, Preferences: rig.store, Now: rig.clock.now})
	ctx := context.Background()

	b, err := broadcasts.Create(ctx, "admin-1", notification.BroadcastDraft{
		Target: notification.BroadcastTarget{WorkspaceIDs: []string{"ws-1"}}, Audience: notification.Everyone(),
		Category: notification.CategoryAnnouncements, Title: "Manutenção", ImageURL: ownedImagePrefix + "1.png",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := broadcasts.Expand(ctx, b.ID); err != nil {
		t.Fatalf("expand: %v", err)
	}
	if len(rig.store.records) != 1 {
		t.Fatalf("records = %d", len(rig.store.records))
	}

	if _, err := broadcasts.Create(ctx, "admin-1", notification.BroadcastDraft{Target: notification.BroadcastTarget{}, Audience: notification.Everyone(), Category: notification.CategoryAnnouncements, Title: "x"}); !errors.Is(err, notification.ErrTargetInvalid) {
		t.Fatalf("no target = %v", err)
	}
	if _, err := broadcasts.Create(ctx, "admin-1", notification.BroadcastDraft{Target: notification.BroadcastTarget{All: true}, Audience: notification.Everyone(), Category: notification.CategoryAnnouncements, Title: "x", ImageURL: "https://evil.example/a.png"}); !errors.Is(err, notification.ErrImageForeign) {
		t.Fatalf("foreign image = %v", err)
	}
	if _, err := broadcasts.List(ctx, "garbage!", 10); !errors.Is(err, notification.ErrCursorInvalid) {
		t.Fatalf("bad cursor = %v", err)
	}
}

func TestImageUploadClassifiesThenStores(t *testing.T) {
	store := &ownedImages{}
	images := NewImages(store)
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 16)...)
	url, err := images.Upload(context.Background(), png)
	if err != nil || url != ownedImagePrefix+"1.png" {
		t.Fatalf("upload = %q, %v", url, err)
	}
	if _, err := images.Upload(context.Background(), []byte("GIF? no, text")); !errors.Is(err, notification.ErrImageType) {
		t.Fatalf("text = %v", err)
	}
	if len(store.saved) != 1 {
		t.Fatal("refused files are never stored")
	}
}
