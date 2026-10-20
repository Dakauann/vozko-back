package notification_repository_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"vozko/domain/notification"
	"vozko/infra/database/schema"
	notification_repository "vozko/infra/repositories/notification"
	"vozko/infra/repositories/repotest"
	notification_usecase "vozko/usecases/notification"
)

type subjectMailbox struct{ subjects []string }

func (m *subjectMailbox) Notify(n notification.Notification) error {
	m.subjects = append(m.subjects, n.Subject)
	return nil
}

func TestAPersistentSituationEmailsThreeTimesAndKeepsOneBellEntryEndToEnd(t *testing.T) {
	db := repotest.IsolatedDB(t, "episodeflow",
		&schema.User{}, &schema.Workspace{}, &schema.WorkspaceCustomRole{}, &schema.WorkspaceMember{},
		&schema.Notification{}, &schema.NotificationRecipient{}, &schema.NotificationPreference{}, &schema.NotificationEpisode{},
	)
	ctx := context.Background()
	owner := schema.User{Username: "ana", Email: "ana@x.com", Password: "x"}
	if err := db.Create(&owner).Error; err != nil {
		t.Fatal(err)
	}
	ws := schema.Workspace{OwnerID: owner.ID, Name: "Loja"}
	if err := db.Create(&ws).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&schema.WorkspaceMember{WorkspaceID: ws.ID, UserID: owner.ID, Role: "owner"}).Error; err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	store := notification_repository.NewStore(db)
	preferences := notification_repository.NewPreferenceStore(db)
	queue, mail := &syncQueue{}, &subjectMailbox{}
	publisher, err := notification_usecase.NewPublisher(notification_usecase.PublisherDeps{
		Outbox: store, Queue: queue, Images: noImages{}, Episodes: notification_repository.NewEpisodeStore(db), Now: clock,
	})
	if err != nil {
		t.Fatal(err)
	}
	delivery, err := notification_usecase.NewDelivery(notification_usecase.DeliveryDeps{
		Outbox: store, Directory: notification_repository.NewDirectory(db), Permissions: managersOnly{},
		Preferences: preferences, Pusher: &countingPusher{}, Email: mail, Digests: store, DashboardURL: "https://app.vozko.test", Now: clock,
	})
	if err != nil {
		t.Fatal(err)
	}
	inbox := notification_usecase.NewInboxService(store, preferences, nil)
	viewer := notification.Viewer{WorkspaceID: ws.ID, UserID: owner.ID}

	draft := notification.Draft{
		WorkspaceID: ws.ID, Category: notification.CategoryAdvertising, Severity: notification.SeverityCritical,
		Title: "Os anúncios pararam: os fundos acabaram", Body: "Os fundos acabaram.", Audience: notification.Managers(), Email: true,
		ActionURL: "/dashboard/advertising", EpisodeKey: "meta_ads_funds:acc-1:funds_out",
	}
	deliverAll := func() {
		for _, id := range queue.fanouts {
			if err := delivery.Deliver(ctx, id); err != nil {
				t.Fatalf("deliver: %v", err)
			}
		}
		queue.fanouts = nil
	}
	for elapsed := time.Duration(0); elapsed < 10*24*time.Hour; elapsed += 2 * time.Hour {
		if _, err := publisher.Publish(ctx, draft); err != nil {
			t.Fatal(err)
		}
		deliverAll()
		now = now.Add(2 * time.Hour)
	}
	if len(mail.subjects) != 3 || !strings.HasPrefix(mail.subjects[2], "Último aviso: ") {
		t.Fatalf("subjects %v", mail.subjects)
	}
	page, err := inbox.List(ctx, viewer, notification.ListQuery{})
	if err != nil || len(page.Items) != 1 || !strings.HasPrefix(page.Items[0].Title, "Último aviso: ") {
		t.Fatalf("the bell keeps only the latest step: %+v %v", page, err)
	}
	if summary, _ := inbox.Summary(ctx, viewer); summary.Unread != 1 {
		t.Fatalf("unread %+v", summary)
	}

	if err := publisher.Resolve(ctx, ws.ID, draft.EpisodeKey); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Hour)
	if _, err := publisher.Publish(ctx, draft); err != nil {
		t.Fatal(err)
	}
	deliverAll()
	if len(mail.subjects) != 4 || mail.subjects[3] != draft.Title {
		t.Fatalf("a new occurrence after funds were added starts over: %v", mail.subjects)
	}
}
