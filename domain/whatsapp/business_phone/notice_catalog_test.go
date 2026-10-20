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

func TestEachRecoveryNoticeEndsTheSituationsItReplaces(t *testing.T) {
	cases := []struct {
		name   string
		notice PhoneNotice
		ends   []string
	}{
		{"a restricted number is no longer only flagged", QualityNotice(true), []string{QualityNotice(false).State}},
		{"a flagged number is no longer restricted", QualityNotice(false), []string{QualityNotice(true).State}},
		{"a reinstated account is no longer banned nor scheduled for disable", ReinstatedNotice(), []string{BannedNotice().State, ScheduledDisableNotice("").State}},
		{"a live number ends a failed onboarding", NumberLiveNotice(), []string{OnboardingFailedNotice("").State}},
		{"an approved name ends a rejection", NameApprovedNotice("Loja"), []string{NameRejectedNotice().State}},
	}
	for _, c := range cases {
		ends := map[string]bool{}
		for _, state := range c.notice.Ends {
			ends[state] = true
		}
		for _, want := range c.ends {
			if !ends[want] {
				t.Errorf("%s: %q does not end %q (ends %v)", c.name, c.notice.State, want, c.notice.Ends)
			}
		}
		if ends[c.notice.State] {
			t.Errorf("%s: a notice never ends its own situation", c.name)
		}
	}
}

func TestQualityAndBanStatesNameTheSituationsARecoveryEnds(t *testing.T) {
	if len(QualityNoticeStates) != 2 || QualityNoticeStates[0] != QualityNotice(false).State || QualityNoticeStates[1] != QualityNotice(true).State {
		t.Fatalf("quality states %v", QualityNoticeStates)
	}
	if SuspendedNotice().State != SuspendedNoticeState {
		t.Fatalf("suspended state %q", SuspendedNotice().State)
	}
}

func TestProblemsPersistUntilResolvedWhileGoodNewsIsAOneOffNotice(t *testing.T) {
	for _, n := range []PhoneNotice{QualityNotice(false), QualityNotice(true), NameRejectedNotice(), BannedNotice(), ScheduledDisableNotice("30/10/2026"), SuspendedNotice(), OnboardingFailedNotice("x")} {
		if !n.Persists() {
			t.Errorf("%s is a problem that lasts until it is fixed", n.State)
		}
	}
	for _, n := range []PhoneNotice{NameApprovedNotice("Loja"), VerifiedNotice(), ReinstatedNotice(), NumberLiveNotice()} {
		if n.Persists() {
			t.Errorf("%s is good news told once, never a reminder", n.State)
		}
	}
}
