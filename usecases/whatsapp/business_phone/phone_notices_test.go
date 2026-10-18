package businessphone_usecase

import (
	"strings"
	"testing"
	"time"

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
	if d.DedupKey != "phone_alert:banned:ph-1" || d.DedupWindow != 7*24*time.Hour {
		t.Fatalf("dedup %q %v", d.DedupKey, d.DedupWindow)
	}
	email := d.EmailCopy
	if email == nil || email.Template != "whatsapp_number_banned.html" || !strings.HasPrefix(email.Subject, "Número de WhatsApp bloqueado - ") ||
		email.Placeholders["PhoneNumber"] != "+55 11 90000-0000" || email.Placeholders["Reason"] != "spam" ||
		email.Placeholders["DashboardURL"] != "https://app.vozko.test/dashboard/whatsapp-business-phones" {
		t.Fatalf("email = %+v", email)
	}
}

func TestAPhoneNoticeKeepsItsOwnWindowAndIgnoresUnownedNumbers(t *testing.T) {
	publisher := &notificationtest.Publisher{}
	notices := NewPhoneNotices(publisher, "https://app.vozko.test")
	notice := bannedNotice()
	notice.Window = time.Hour

	if err := notices.NotifyPhone(&businessphone.WhatsAppBusinessPhoneNumber{ID: "ph-1"}, notice); err != nil {
		t.Fatal(err)
	}
	if err := notices.NotifyPhone(nil, notice); err != nil {
		t.Fatal(err)
	}
	if len(publisher.Drafts()) != 0 {
		t.Fatal("a number without a workspace notifies nobody")
	}
	_ = notices.NotifyPhone(&businessphone.WhatsAppBusinessPhoneNumber{ID: "ph-2", OwnerWorkspaceID: "ws-1"}, notice)
	if d := publisher.Only(t); d.DedupWindow != time.Hour {
		t.Fatalf("window = %v", d.DedupWindow)
	}
}
