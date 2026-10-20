package businessphone_usecase

import (
	"strings"
	"testing"

	"vozko/domain/notification"
	businessphone "vozko/domain/whatsapp/business_phone"
	"vozko/domain/workspace"
	"vozko/usecases/notification/notificationtest"
)

func bannedNotice() businessphone.PhoneNotice {
	return businessphone.PhoneNotice{
		State: "banned", Severity: notification.SeverityCritical,
		Title: "Número de WhatsApp bloqueado", Body: "A Meta bloqueou o número.",
		Subject: "Número de WhatsApp bloqueado", Template: "whatsapp_number_banned.html",
		Extra: map[string]interface{}{"Reason": "spam"},
	}
}

func TestAPhoneNoticeReachesWhoSeesTheNumbersWithTheNumberNamed(t *testing.T) {
	publisher := &notificationtest.Publisher{}
	notices := NewPhoneNotices(publisher, "https://app.vozko.test/")
	phone := &businessphone.WhatsAppBusinessPhoneNumber{ID: "ph-1", OwnerWorkspaceID: "ws-1", DisplayPhoneNumber: "+55 11 90000-0000"}

	if err := notices.NotifyPhone(phone, bannedNotice()); err != nil {
		t.Fatalf("notify: %v", err)
	}
	d := publisher.Only(t)
	if d.WorkspaceID != "ws-1" || d.Category != notification.CategoryChannels || d.Severity != notification.SeverityCritical || !d.Email {
		t.Fatalf("draft = %+v", d)
	}
	if !d.Audience.Equal(notification.Holding(workspace.ResourceBusinessPhones, workspace.ActionRead)) {
		t.Fatalf("audience = %+v", d.Audience)
	}
	if !strings.Contains(d.Body, "+55 11 90000-0000") || d.ActionURL != "/dashboard/whatsapp-business-phones" {
		t.Fatalf("body %q link %q", d.Body, d.ActionURL)
	}
	if d.EpisodeKey != "phone_alert:banned:ph-1" || d.DedupKey != "" {
		t.Fatalf("episode %q dedup %q", d.EpisodeKey, d.DedupKey)
	}
	email := d.EmailCopy
	if email == nil || email.Template != "whatsapp_number_banned.html" || !strings.HasPrefix(email.Subject, "Número de WhatsApp bloqueado - ") ||
		email.Placeholders["PhoneNumber"] != "+55 11 90000-0000" || email.Placeholders["Reason"] != "spam" ||
		email.Placeholders["DashboardURL"] != "https://app.vozko.test/dashboard/whatsapp-business-phones" {
		t.Fatalf("email = %+v", email)
	}
}

func TestAPhoneNoticeIgnoresUnownedNumbers(t *testing.T) {
	publisher := &notificationtest.Publisher{}
	notices := NewPhoneNotices(publisher, "https://app.vozko.test")
	notice := bannedNotice()

	if err := notices.NotifyPhone(&businessphone.WhatsAppBusinessPhoneNumber{ID: "ph-1"}, notice); err != nil {
		t.Fatal(err)
	}
	if err := notices.NotifyPhone(nil, notice); err != nil {
		t.Fatal(err)
	}
	if len(publisher.Drafts()) != 0 {
		t.Fatal("a number without a workspace notifies nobody")
	}
	if err := notices.ClearPhone(&businessphone.WhatsAppBusinessPhoneNumber{ID: "ph-1"}, "banned"); err != nil || len(publisher.Resolutions()) != 0 {
		t.Fatalf("a number without a workspace has nothing to resolve: %v", err)
	}
}

func TestANoticeEndsTheSituationsItReplacesBeforeNotifying(t *testing.T) {
	publisher := &notificationtest.Publisher{}
	phone := &businessphone.WhatsAppBusinessPhoneNumber{ID: "ph-1", OwnerWorkspaceID: "ws-1"}
	if err := NewPhoneNotices(publisher, "https://app.vozko.test").NotifyPhone(phone, businessphone.QualityNotice(true)); err != nil {
		t.Fatal(err)
	}
	resolved := publisher.Resolutions()
	if len(resolved) != 1 || resolved[0].WorkspaceID != "ws-1" || len(resolved[0].Keys) != 1 || resolved[0].Keys[0] != "phone_alert:quality:FLAGGED:ph-1" {
		t.Fatalf("resolved %+v", resolved)
	}
	if d := publisher.Only(t); d.EpisodeKey != "phone_alert:quality:RESTRICTED:ph-1" {
		t.Fatalf("a restricted number notifies at once as its own episode, got %q", d.EpisodeKey)
	}
}

func TestClearingANumberResolvesTheNamedSituationsOfThatNumber(t *testing.T) {
	publisher := &notificationtest.Publisher{}
	phone := &businessphone.WhatsAppBusinessPhoneNumber{ID: "ph-1", OwnerWorkspaceID: "ws-1"}
	if err := NewPhoneNotices(publisher, "https://app.vozko.test").ClearPhone(phone, businessphone.QualityNoticeStates...); err != nil {
		t.Fatal(err)
	}
	keys := publisher.ResolvedKeys()
	if len(keys) != 2 || !keys["phone_alert:quality:FLAGGED:ph-1"] || !keys["phone_alert:quality:RESTRICTED:ph-1"] {
		t.Fatalf("resolved %v", keys)
	}
}

func TestGoodNewsIsPublishedAsAOneOffNoticeWithoutAnEpisode(t *testing.T) {
	publisher := &notificationtest.Publisher{}
	phone := &businessphone.WhatsAppBusinessPhoneNumber{ID: "ph-1", OwnerWorkspaceID: "ws-1"}
	if err := NewPhoneNotices(publisher, "https://app.vozko.test").NotifyPhone(phone, businessphone.NumberLiveNotice()); err != nil {
		t.Fatal(err)
	}
	if d := publisher.Only(t); d.EpisodeKey != "" || d.DedupKey != "" {
		t.Fatalf("a live number is told once per transition, got episode %q dedup %q", d.EpisodeKey, d.DedupKey)
	}
	if !publisher.ResolvedKeys()["phone_alert:onboarding_failed:ph-1"] {
		t.Fatal("going live still ends a failed onboarding")
	}
}
