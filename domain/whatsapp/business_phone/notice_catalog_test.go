package businessphone

import (
	"testing"

	"vozko/domain/notification"
)

func TestEveryPhoneNoticeIsAValidNotificationWithItsOwnState(t *testing.T) {
	notices := []PhoneNotice{
		QualityNotice(false), QualityNotice(true), NameApprovedNotice("Loja"), NameRejectedNotice(), VerifiedNotice(),
		ReinstatedNotice(), BannedNotice(), ScheduledDisableNotice("30/10/2026"), SuspendedNotice(), NumberLiveNotice(),
		OnboardingFailedNotice("token expirado"),
	}
	seen := map[string]bool{}
	for _, n := range notices {
		if seen[n.State] {
			t.Fatalf("state %q is used twice, so their dedup keys would collide", n.State)
		}
		seen[n.State] = true
		draft := notification.Draft{
			WorkspaceID: "ws-1", Category: notification.CategoryChannels, Severity: n.Severity,
			Title: n.Title, Body: n.Body, Audience: notification.Everyone(),
		}
		if err := draft.Normalize().Validate(); err != nil || n.Template == "" || n.Subject == "" {
			t.Fatalf("%s: invalid notice %+v: %v", n.State, n, err)
		}
	}
	if QualityNotice(true).Severity != notification.SeverityCritical || QualityNotice(false).Severity != notification.SeverityWarning {
		t.Fatal("a restricted number is critical, a flagged one a warning")
	}
}
