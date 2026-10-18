package notification_repository_test

import (
	"context"
	"sync"
	"testing"

	"vozko/domain/notification"
	"vozko/domain/workspace"
	"vozko/infra/database/schema"
	notification_repository "vozko/infra/repositories/notification"
	"vozko/infra/repositories/repotest"
	notification_usecase "vozko/usecases/notification"
)

type syncQueue struct {
	mu      sync.Mutex
	fanouts []string
	bcasts  []string
}

func (q *syncQueue) EnqueueFanout(_ context.Context, id string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.fanouts = append(q.fanouts, id)
	return nil
}

func (q *syncQueue) EnqueueBroadcast(_ context.Context, id string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.bcasts = append(q.bcasts, id)
	return nil
}

type noImages struct{}

func (noImages) Save(context.Context, []byte, notification.Image) (string, error) { return "", nil }
func (noImages) Owns(string) bool                                                 { return false }

type countingPusher struct{ users int }

func (p *countingPusher) PushNotification(_ string, ids []string, _ notification.Item) {
	p.users += len(ids)
}

type mailbox struct{ to []string }

func (m *mailbox) Notify(n notification.Notification) error {
	m.to = append(m.to, n.Email)
	return nil
}

type managersOnly struct{}

func (managersOnly) HasWorkspacePermission(string, string, string, string, bool) bool { return false }

func TestPublishedNotificationReachesTheRightInboxesEndToEnd(t *testing.T) {
	db := repotest.IsolatedDB(t, "notificationflow",
		&schema.User{}, &schema.Workspace{}, &schema.WorkspaceCustomRole{}, &schema.WorkspaceMember{},
		&schema.Notification{}, &schema.NotificationRecipient{}, &schema.NotificationPreference{}, &schema.NotificationBroadcast{},
	)
	ctx := context.Background()

	owner := schema.User{Username: "ana", Email: "ana@x.com", Password: "x"}
	agent := schema.User{Username: "beto", Email: "beto@x.com", Password: "x"}
	for _, u := range []*schema.User{&owner, &agent} {
		if err := db.Create(u).Error; err != nil {
			t.Fatal(err)
		}
	}
	ws := schema.Workspace{OwnerID: owner.ID, Name: "Loja"}
	if err := db.Create(&ws).Error; err != nil {
		t.Fatal(err)
	}
	for _, m := range []schema.WorkspaceMember{{WorkspaceID: ws.ID, UserID: owner.ID, Role: "owner"}, {WorkspaceID: ws.ID, UserID: agent.ID, Role: "member"}} {
		if err := db.Create(&m).Error; err != nil {
			t.Fatal(err)
		}
	}

	store := notification_repository.NewStore(db)
	preferences := notification_repository.NewPreferenceStore(db)
	queue, pusher, mail := &syncQueue{}, &countingPusher{}, &mailbox{}
	publisher, err := notification_usecase.NewPublisher(notification_usecase.PublisherDeps{Outbox: store, Queue: queue, Images: noImages{}})
	if err != nil {
		t.Fatal(err)
	}
	delivery, err := notification_usecase.NewDelivery(notification_usecase.DeliveryDeps{
		Outbox: store, Directory: notification_repository.NewDirectory(db), Permissions: managersOnly{},
		Preferences: preferences, Pusher: pusher, Email: mail, Digests: store, DashboardURL: "https://app.vozko.test",
	})
	if err != nil {
		t.Fatal(err)
	}
	inbox := notification_usecase.NewInboxService(store, preferences, nil)

	if _, err := inbox.SavePreferences(ctx, notification.Viewer{WorkspaceID: ws.ID, UserID: agent.ID}, notification_usecase.PreferencesChange{EmailMuted: []string{"campaigns"}}); err != nil {
		t.Fatal(err)
	}

	draft := notification.Draft{
		WorkspaceID: ws.ID, Category: notification.CategoryCampaigns, Severity: notification.SeverityWarning,
		Title: "Campanha pausada", Body: "O modelo foi pausado.", Audience: notification.Everyone(), Email: true,
		DedupKey: "hold:c-1", ActionURL: "/dashboard/whatsapp-campaigns/c-1",
	}
	rec, err := publisher.Publish(ctx, draft)
	if err != nil {
		t.Fatal(err)
	}
	if again, _ := publisher.Publish(ctx, draft); again.ID != rec.ID || len(queue.fanouts) != 1 {
		t.Fatal("the same event published twice is one notification")
	}
	if err := delivery.Deliver(ctx, queue.fanouts[0]); err != nil {
		t.Fatalf("deliver: %v", err)
	}

	if pusher.users != 2 || len(mail.to) != 1 || mail.to[0] != "ana@x.com" {
		t.Fatalf("pushed to %d, mailed %v; the agent muted campaigns by email", pusher.users, mail.to)
	}
	for _, person := range []string{owner.ID, agent.ID} {
		viewer := notification.Viewer{WorkspaceID: ws.ID, UserID: person}
		page, err := inbox.List(ctx, viewer, notification.ListQuery{})
		if err != nil || len(page.Items) != 1 || page.Items[0].ID != rec.ID || page.Items[0].ReadAt != nil {
			t.Fatalf("inbox of %s = %+v, %v", person, page, err)
		}
	}
	viewer := notification.Viewer{WorkspaceID: ws.ID, UserID: agent.ID}
	if n, err := inbox.MarkRead(ctx, viewer, []string{rec.ID}); err != nil || n != 1 {
		t.Fatalf("mark read = %d, %v", n, err)
	}
	summary, _ := inbox.Summary(ctx, viewer)
	ownerSummary, _ := inbox.Summary(ctx, notification.Viewer{WorkspaceID: ws.ID, UserID: owner.ID})
	if summary.Unread != 0 || ownerSummary.Unread != 1 {
		t.Fatalf("read state is per person: agent %+v owner %+v", summary, ownerSummary)
	}

	permissionDraft := draft
	permissionDraft.DedupKey = "hold:c-2"
	permissionDraft.Audience = notification.Holding(workspace.ResourceWhatsAppCampaigns, workspace.ActionUpdate)
	second, _ := publisher.Publish(ctx, permissionDraft)
	if err := delivery.Deliver(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	stored, _ := store.Get(ctx, second.ID)
	if stored.Status != notification.StatusDelivered || stored.RecipientCount != 0 {
		t.Fatalf("nobody holds the permission, so nobody receives it: %+v", stored)
	}
}
