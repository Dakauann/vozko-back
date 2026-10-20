package mercadolivre

import (
	"context"
	"strings"
	"testing"
	"time"

	"vozko/brand"
	mldomain "vozko/domain/mercadolivre"
	"vozko/domain/notification"
	"vozko/domain/workspace"
	"vozko/usecases/notification/notificationtest"
)

func TestReconnectAlertIsAPersistentChannelEpisode(t *testing.T) {
	brand.SetForTest(brand.Brand{Name: "Vozko", Gender: brand.Feminine})
	publisher := &notificationtest.Publisher{}
	alerts := NewAccountAlerts(publisher)
	account := connectedAccount()
	account.Status = mldomain.StatusNeedsReconnect

	if err := alerts.NeedsReconnect(context.Background(), account); err != nil {
		t.Fatal(err)
	}
	drafts := publisher.Drafts()
	if len(drafts) != 1 {
		t.Fatalf("drafts = %d", len(drafts))
	}
	d := drafts[0]
	if d.Category != notification.CategoryChannels || d.Severity != notification.SeverityCritical || !d.Email {
		t.Fatalf("draft = %+v", d)
	}
	if d.EpisodeKey != "mercadolivre_reconnect:acct-1" || d.DedupKey != "" || d.Source != notification.SourceMercadoLivre {
		t.Fatalf("episode = %q dedup = %q source = %q", d.EpisodeKey, d.DedupKey, d.Source)
	}
	if d.Audience.Permission == nil || d.Audience.Permission.Resource != workspace.ResourceMercadoLivreAccounts || d.Audience.Permission.Action != workspace.ActionCreate {
		t.Fatalf("audience = %+v", d.Audience)
	}
	if d.ActionURL != "/dashboard/mercadolivre-accounts/acct-1" || !strings.Contains(d.Title, "LOJA") || !strings.Contains(d.Body, "na Vozko") {
		t.Fatalf("copy = %q / %q / %q", d.Title, d.Body, d.ActionURL)
	}
	if err := d.Normalize().Validate(); err != nil {
		t.Fatalf("draft invalid: %v", err)
	}
	reminder := d.ForEpisodeStep(notification.Reminder, testNow, time.UTC)
	last := d.ForEpisodeStep(notification.LastNotice, testNow, time.UTC)
	if reminder.Title == d.Title || last.Title == reminder.Title || !strings.Contains(last.Body, "último aviso") {
		t.Fatalf("the three notices must read differently: %q / %q / %q", d.Title, reminder.Title, last.Title)
	}

	if err := alerts.Reconnected(context.Background(), account); err != nil {
		t.Fatal(err)
	}
	if !publisher.ResolvedKeys()["mercadolivre_reconnect:acct-1"] {
		t.Fatalf("reconnecting must resolve the episode: %+v", publisher.Resolutions())
	}
}

func TestARevokedAccountAlertNamesThePurge(t *testing.T) {
	brand.SetForTest(brand.Brand{Name: "Vozko", Gender: brand.Feminine})
	publisher := &notificationtest.Publisher{}
	account := connectedAccount()
	account.Status = mldomain.StatusRevoked
	purge := testNow.Add(30 * 24 * time.Hour)
	account.PurgeAfter = &purge
	if err := NewAccountAlerts(publisher).NeedsReconnect(context.Background(), account); err != nil {
		t.Fatal(err)
	}
	d := publisher.Drafts()[0]
	if !strings.Contains(d.Body, "18/11/2026") {
		t.Fatalf("body = %q", d.Body)
	}
}

func TestAConnectedAccountRaisesNoAlert(t *testing.T) {
	publisher := &notificationtest.Publisher{}
	if err := NewAccountAlerts(publisher).NeedsReconnect(context.Background(), connectedAccount()); err != nil {
		t.Fatal(err)
	}
	if len(publisher.Drafts()) != 0 {
		t.Fatal("a connected account must not alert")
	}
}
