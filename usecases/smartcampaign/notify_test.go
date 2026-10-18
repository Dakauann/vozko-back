package smartcampaign_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"vozko/domain/notification"
	"vozko/domain/workspace"
	wc "vozko/domain/whatsapp_campaign"
	sc "vozko/domain/whatsapp_campaign/smartcampaign"
)

type recordingPublisher struct {
	drafts []notification.Draft
	err    error
}

func (r *recordingPublisher) Publish(_ context.Context, d notification.Draft) (notification.Record, error) {
	r.drafts = append(r.drafts, d)
	return notification.Record{Draft: d, ID: "n-1"}, r.err
}

func heldYellow() (sc.Decision, time.Time) {
	held := time.Date(2026, 10, 16, 15, 56, 0, 0, time.UTC)
	return sc.Decision{Action: sc.Pause, Findings: []sc.Finding{{Rule: sc.RuleTemplateYellow, Action: sc.Pause, At: held}}}, held
}

func TestTheCampaignTeamIsNotifiedWhyTheCampaignWasHeld(t *testing.T) {
	sent := &recordingPublisher{}
	notice := NewHoldNotice(sent, "https://app.example.com/")
	d, held := heldYellow()

	notice.Held(&wc.Campaign{ID: "c1", WorkspaceID: "ws", Name: "Promo outubro"}, 3, d, held.Add(time.Minute))

	if len(sent.drafts) != 1 {
		t.Fatalf("published %d notifications, want 1", len(sent.drafts))
	}
	n := sent.drafts[0]
	if n.WorkspaceID != "ws" || n.Category != notification.CategoryCampaigns || n.Severity != notification.SeverityWarning || !n.Email {
		t.Fatalf("draft = %+v", n)
	}
	want := notification.Holding(workspace.ResourceWhatsAppCampaigns, workspace.ActionUpdate)
	if n.Audience.Kind != want.Kind || *n.Audience.Permission != *want.Permission {
		t.Fatalf("audience = %+v, want the people who can manage campaigns", n.Audience)
	}
	if n.DedupKey != "smart_campaign_hold:c1:template_yellow" || n.DedupWindow != 24*time.Hour || n.Source != notification.SourceSmartCampaign {
		t.Fatalf("dedup = %q %v %q", n.DedupKey, n.DedupWindow, n.Source)
	}
	if n.ActionURL != "/dashboard/whatsapp-campaigns/c1" || n.ActionLabel != "Ver campanha" {
		t.Fatalf("link = %q %q", n.ActionURL, n.ActionLabel)
	}
	if !strings.Contains(n.Title, "Promo outubro") || !strings.Contains(n.Body, "AMARELO") {
		t.Fatalf("title %q body %q", n.Title, n.Body)
	}
	if err := n.Normalize().Validate(); err != nil {
		t.Fatalf("the draft must be valid: %v", err)
	}

	copy := n.EmailCopy
	if copy == nil || copy.Template != "whatsapp_campaign_hold.html" || copy.Subject != n.Title {
		t.Fatalf("email copy = %+v", copy)
	}
	if reason := copy.Placeholders["Reason"].(string); !strings.Contains(reason, "AMARELO") {
		t.Fatalf("reason = %q, want the yellow template named", reason)
	}
	if copy.Placeholders["HeldAt"] != "16/10 12:56" {
		t.Fatalf("held at = %v, want the time the template turned yellow, in Brasília", copy.Placeholders["HeldAt"])
	}
	if copy.Placeholders["DashboardURL"] != "https://app.example.com/dashboard/whatsapp-campaigns/c1" {
		t.Fatalf("link = %v", copy.Placeholders["DashboardURL"])
	}
}

func TestAStoppedCampaignIsCriticalAndLongNamesStillFit(t *testing.T) {
	sent := &recordingPublisher{}
	notice := NewHoldNotice(sent, "https://app.example.com")
	held := time.Date(2026, 10, 16, 15, 56, 0, 0, time.UTC)
	d := sc.Decision{Action: sc.Stop, Findings: []sc.Finding{{Rule: sc.RuleSpamLimit, Action: sc.Stop, At: held}}}

	notice.Held(&wc.Campaign{ID: "c2", WorkspaceID: "ws", Name: strings.Repeat("Campanha de fim de ano ", 20)}, 0, d, held)

	n := sent.drafts[0]
	if n.Severity != notification.SeverityCritical || !strings.HasPrefix(n.Title, "Campanha encerrada") {
		t.Fatalf("draft = %+v", n)
	}
	if utf8.RuneCountInString(n.Title) > notification.TitleMaxRunes {
		t.Fatalf("title has %d runes", utf8.RuneCountInString(n.Title))
	}
	if err := n.Normalize().Validate(); err != nil {
		t.Fatalf("a long campaign name must not lose the notification: %v", err)
	}
}

func TestAPublishFailureIsOnlyLogged(t *testing.T) {
	notice := NewHoldNotice(&recordingPublisher{err: errors.New("db down")}, "https://app.example.com")
	d, held := heldYellow()
	notice.Held(&wc.Campaign{ID: "c1", WorkspaceID: "ws", Name: "Promo"}, 1, d, held)
}

func TestEveryRuleHasAReasonInPortuguese(t *testing.T) {
	rules := []sc.Rule{
		sc.RuleSpamLimit, sc.RuleAccountLocked, sc.RulePaymentProblem, sc.RuleAccessRemoved, sc.RuleTemplateDisabled,
		sc.RuleTemplatePaused, sc.RuleTemplateInReview, sc.RuleTemplateRed, sc.RulePhoneBanned, sc.RulePhoneUnavailable,
		sc.RuleTemplateYellow, sc.RuleCategoryMarketing, sc.RuleTemplateFlagged, sc.RuleFailureRateStop, sc.RuleFailureRateSlow,
		sc.RuleEngagementLimit, sc.RulePersistentSlowDown, sc.RuleHealthUnavailable, sc.RuleMonthlyCap, sc.RuleOutsideSendHours,
		sc.RuleSendHoursInvalid, sc.RuleDailyWaveLimit,
	}
	for _, rule := range rules {
		if reason := Reason(sc.Finding{Rule: rule, Observed: 0.12, Limit: 0.08}); reason == "" || reason == string(rule) {
			t.Fatalf("rule %s has no reason text", rule)
		}
	}
}
