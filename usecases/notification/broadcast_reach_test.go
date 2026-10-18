package notification_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/notification"
)

func reachDirectory() fixedDirectory {
	return fixedDirectory{
		workspaces: []string{"ws-1", "ws-2", "ws-3"},
		members: map[string][]notification.Member{
			"ws-1": {{UserID: "owner", Email: "owner@x.com", Manager: true}, {UserID: "agent", Email: "agent@x.com"}},
			"ws-2": {{UserID: "owner", Email: "owner@x.com", Manager: true}, {UserID: "solo", Email: "solo@x.com", Manager: true}},
			"ws-3": {{UserID: "lonely", Email: "lonely@x.com"}},
		},
	}
}

func newReachRig(t *testing.T) (*centerRig, *Broadcasts) {
	t.Helper()
	rig := newCenterRig(t, reachDirectory(), nil)
	broadcasts, err := NewBroadcasts(BroadcastsDeps{
		Store: rig.store, Directory: reachDirectory(), Publisher: rig.publisher, Queue: rig.queue, Images: rig.images,
		Preferences: rig.store, Now: rig.clock.now, PageSize: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	return rig, broadcasts
}

func managersAnnouncement() notification.BroadcastDraft {
	return notification.BroadcastDraft{
		Target: notification.BroadcastTarget{All: true}, Audience: notification.Managers(),
		Category: notification.CategoryAnnouncements, Title: "Novidade", Email: true,
	}
}

func TestReachCountsWorkspacesDeliveriesPeopleAndEmailsTheWayDeliveryWould(t *testing.T) {
	rig, broadcasts := newReachRig(t)
	rig.store.prefs["ws-2/solo"] = notification.Preferences{EmailMuted: []notification.Category{notification.CategoryAnnouncements}}

	reach, err := broadcasts.Preview(context.Background(), managersAnnouncement())
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	want := notification.BroadcastReach{Workspaces: 2, Recipients: 3, People: 2, Emails: 1}
	if reach != want {
		t.Fatalf("reach = %+v, want %+v", reach, want)
	}

	critical := managersAnnouncement()
	critical.Severity = notification.SeverityCritical
	if reach, _ := broadcasts.Preview(context.Background(), critical); reach.Emails != 2 {
		t.Fatalf("critical ignores mutes, emails = %d", reach.Emails)
	}
	silent := managersAnnouncement()
	silent.Email = false
	if reach, _ := broadcasts.Preview(context.Background(), silent); reach.Emails != 0 || reach.People != 2 {
		t.Fatalf("no email asked, reach = %+v", reach)
	}
	listed := managersAnnouncement()
	listed.Target, listed.Audience = notification.BroadcastTarget{WorkspaceIDs: []string{"ws-1", "gone"}}, notification.Everyone()
	if reach, _ := broadcasts.Preview(context.Background(), listed); reach != (notification.BroadcastReach{Workspaces: 1, Recipients: 2, People: 2, Emails: 2}) {
		t.Fatalf("listed reach = %+v", reach)
	}
	if len(rig.store.records) != 0 || len(rig.queue.broadcasts) != 0 {
		t.Fatal("a preview never stores or queues anything")
	}
}

func TestReachRefusesTheSameDraftsCreateRefuses(t *testing.T) {
	_, broadcasts := newReachRig(t)
	broken := managersAnnouncement()
	broken.Title = ""
	if _, err := broadcasts.Preview(context.Background(), broken); !errors.Is(err, notification.ErrTitleRequired) {
		t.Fatalf("preview of an invalid draft = %v", err)
	}
}

func TestBroadcastListCarriesHowManyReceivedAndReadAndWhoDid(t *testing.T) {
	rig, broadcasts := newReachRig(t)
	ctx := context.Background()
	b, _ := broadcasts.Create(ctx, "admin-1", managersAnnouncement())
	if err := broadcasts.Expand(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	for _, id := range rig.queue.fanouts {
		if err := rig.delivery.Deliver(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	inbox := NewInboxService(rig.store, rig.store, rig.clock.now)
	firstOwnerNotice := ""
	for _, r := range rig.store.recipients {
		if r.UserID == "owner" && r.WorkspaceID == "ws-1" {
			firstOwnerNotice = r.NotificationID
		}
	}
	_, _ = inbox.MarkRead(ctx, notification.Viewer{WorkspaceID: "ws-1", UserID: "owner"}, []string{firstOwnerNotice})

	page, err := broadcasts.List(ctx, "", 10)
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("list = %+v, %v", page, err)
	}
	if stats := page.Items[0].Stats; stats != (notification.BroadcastStats{Recipients: 3, Read: 1}) {
		t.Fatalf("stats = %+v", stats)
	}

	read, err := broadcasts.Recipients(ctx, b.ID, "read", "", 10)
	if err != nil || len(read.Items) != 1 || read.Items[0].UserID != "owner" || read.Items[0].ReadAt == nil {
		t.Fatalf("readers = %+v, %v", read, err)
	}
	first, _ := broadcasts.Recipients(ctx, b.ID, "all", "", 2)
	second, _ := broadcasts.Recipients(ctx, b.ID, "all", first.Next, 2)
	if len(first.Items) != 2 || first.Next == "" || len(second.Items) != 1 || second.Next != "" {
		t.Fatalf("pages = %+v / %+v", first, second)
	}
	if _, err := broadcasts.Recipients(ctx, b.ID, "seen", "", 10); !errors.Is(err, notification.ErrReadStatusUnknown) {
		t.Fatalf("bad status = %v", err)
	}
	if _, err := broadcasts.Recipients(ctx, b.ID, "all", "%%", 10); !errors.Is(err, notification.ErrCursorInvalid) {
		t.Fatalf("bad cursor = %v", err)
	}
	if _, err := broadcasts.Recipients(ctx, "missing", "all", "", 10); !errors.Is(err, notification.ErrNotFound) {
		t.Fatalf("unknown broadcast = %v", err)
	}
}
