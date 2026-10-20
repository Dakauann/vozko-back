package tiktok

import (
	"context"
	"strings"
	"testing"
	"time"

	"vozko/brand"
	"vozko/domain/notification"
	ttdomain "vozko/domain/tiktok"
	"vozko/domain/workspace"
	"vozko/usecases/notification/notificationtest"
)

func alertAccount(status ttdomain.Status) *ttdomain.Account {
	return &ttdomain.Account{ID: "acct-1", WorkspaceID: "ws-1", OpenID: "_000abc", Username: "loja", DisplayName: "LOJA", Status: status}
}

func TestReconnectAlertIsAPersistentChannelEpisode(t *testing.T) {
	brand.SetForTest(brand.Brand{Name: "Vozko", Gender: brand.Feminine})
	publisher := &notificationtest.Publisher{}
	if err := NewAccountAlerts(publisher).NeedsReconnect(context.Background(), alertAccount(ttdomain.StatusNeedsReconnect)); err != nil {
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
	if d.EpisodeKey != "tiktok_reconnect:acct-1" || d.DedupKey != "" || d.Source != notification.SourceTikTok {
		t.Fatalf("episode = %q dedup = %q source = %q", d.EpisodeKey, d.DedupKey, d.Source)
	}
	if d.Audience.Permission == nil || d.Audience.Permission.Resource != workspace.ResourceTikTokAccounts || d.Audience.Permission.Action != workspace.ActionCreate {
		t.Fatalf("audience = %+v", d.Audience)
	}
	if d.ActionURL != "/dashboard/tiktok-accounts/acct-1" || !strings.Contains(d.Title, "LOJA") || !strings.Contains(d.Body, "na Vozko") {
		t.Fatalf("copy = %q / %q / %q", d.Title, d.Body, d.ActionURL)
	}
	if err := d.Normalize().Validate(); err != nil {
		t.Fatalf("draft invalid: %v", err)
	}
	reminder := d.ForEpisodeStep(notification.Reminder, testNow, time.UTC)
	last := d.ForEpisodeStep(notification.LastNotice, testNow, time.UTC)
	if reminder.Title == d.Title || last.Title == reminder.Title {
		t.Fatalf("the three notices must read differently: %q / %q / %q", d.Title, reminder.Title, last.Title)
	}
}

func TestARevokedAccountAlertNamesThePurgeDate(t *testing.T) {
	brand.SetForTest(brand.Brand{Name: "Vozko", Gender: brand.Feminine})
	publisher := &notificationtest.Publisher{}
	account := alertAccount(ttdomain.StatusRevoked)
	purge := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	account.PurgeAfter = &purge
	if err := NewAccountAlerts(publisher).NeedsReconnect(context.Background(), account); err != nil {
		t.Fatal(err)
	}
	if body := publisher.Drafts()[0].Body; !strings.Contains(body, "18/11/2026") {
		t.Fatalf("body = %q", body)
	}
}

func TestAConnectedAccountRaisesNoReconnectAlert(t *testing.T) {
	publisher := &notificationtest.Publisher{}
	if err := NewAccountAlerts(publisher).NeedsReconnect(context.Background(), alertAccount(ttdomain.StatusConnected)); err != nil {
		t.Fatal(err)
	}
	if len(publisher.Drafts()) != 0 {
		t.Fatalf("drafts = %+v", publisher.Drafts())
	}
}

func TestReconnectSoonWarnsByEmailWithTheExpiryDate(t *testing.T) {
	brand.SetForTest(brand.Brand{Name: "Vozko", Gender: brand.Feminine})
	publisher := &notificationtest.Publisher{}
	account := alertAccount(ttdomain.StatusConnected)
	expires := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	account.RefreshExpiresAt = &expires
	if err := NewAccountAlerts(publisher).ReconnectSoon(context.Background(), account); err != nil {
		t.Fatal(err)
	}
	d := publisher.Drafts()[0]
	if d.Severity != notification.SeverityWarning || !d.Email || d.EpisodeKey != "tiktok_reconnect_soon:acct-1" || !strings.Contains(d.Body, "10/11/2026") {
		t.Fatalf("draft = %+v", d)
	}
	if err := d.Normalize().Validate(); err != nil {
		t.Fatalf("draft invalid: %v", err)
	}
	if !publisher.ResolvedKeys()["tiktok_reconnect:acct-1"] || !publisher.ResolvedKeys()["tiktok_webhooks:acct-1"] {
		t.Fatalf("a working account resolves the reconnect and delivery episodes: %+v", publisher.Resolutions())
	}
}

func TestWebhooksUnhealthyIsAnInAppWarning(t *testing.T) {
	brand.SetForTest(brand.Brand{Name: "Vozko", Gender: brand.Feminine})
	publisher := &notificationtest.Publisher{}
	if err := NewAccountAlerts(publisher).WebhooksUnhealthy(context.Background(), alertAccount(ttdomain.StatusConnected)); err != nil {
		t.Fatal(err)
	}
	d := publisher.Drafts()[0]
	if d.Severity != notification.SeverityWarning || d.Email || d.EpisodeKey != "tiktok_webhooks:acct-1" {
		t.Fatalf("draft = %+v", d)
	}
	if err := d.Normalize().Validate(); err != nil {
		t.Fatalf("draft invalid: %v", err)
	}
	if !publisher.ResolvedKeys()["tiktok_reconnect:acct-1"] {
		t.Fatalf("a connected account resolves the reconnect episode: %+v", publisher.Resolutions())
	}
}

func TestHealthyResolvesEveryAccountEpisode(t *testing.T) {
	publisher := &notificationtest.Publisher{}
	if err := NewAccountAlerts(publisher).Healthy(context.Background(), alertAccount(ttdomain.StatusConnected)); err != nil {
		t.Fatal(err)
	}
	resolved := publisher.ResolvedKeys()
	for _, key := range []string{"tiktok_reconnect:acct-1", "tiktok_reconnect_soon:acct-1", "tiktok_webhooks:acct-1"} {
		if !resolved[key] {
			t.Errorf("%s not resolved: %+v", key, publisher.Resolutions())
		}
	}
	if len(publisher.Drafts()) != 0 {
		t.Fatalf("a healthy account publishes nothing: %+v", publisher.Drafts())
	}
}
