package workspace_plan_usecase

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"vozko/domain/notification"
	"vozko/domain/workspace"
	workspace_plan "vozko/domain/workspace/workspace_plan"
	"vozko/usecases/notification/notificationtest"
)

type upcomingExpirations []*workspace_plan.WorkspaceSubscription

func (u upcomingExpirations) ListUpcomingExpirations(time.Time, time.Time, int) ([]*workspace_plan.WorkspaceSubscription, error) {
	return u, nil
}

type namedPlans map[string]string

func (n namedPlans) GetByID(id string) (*workspace_plan.PlanDefinition, error) {
	name, ok := n[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return &workspace_plan.PlanDefinition{ID: id, Name: name}, nil
}

func TestAnExpiringPlanNotifiesWhoCanRenewIt(t *testing.T) {
	end := time.Date(2026, 10, 24, 3, 0, 0, 0, time.UTC)
	publisher := &notificationtest.Publisher{}
	uc := NewRemindExpiringSubscriptionsUseCase(
		upcomingExpirations{{WorkspaceID: "ws-1", PlanDefinitionID: "growth", CurrentPeriodEnd: end}},
		namedPlans{"growth": "Plano Growth"},
		publisher, "https://app.vozko.test/",
	)

	sent, err := uc.RemindExpiring()
	if err != nil || sent != 1 {
		t.Fatalf("sent = %d, %v", sent, err)
	}
	d := publisher.Only(t)
	if d.WorkspaceID != "ws-1" || d.Category != notification.CategoryBilling || d.Severity != notification.SeverityWarning || !d.Email {
		t.Fatalf("draft = %+v", d)
	}
	if !d.Audience.Equal(notification.Holding(workspace.ResourcePlans, workspace.ActionCreate)) {
		t.Fatalf("audience = %+v, want the people who can renew the plan", d.Audience)
	}
	if !strings.Contains(d.Title, "Plano Growth") || !strings.Contains(d.Body, "24/10/2026") || d.ActionURL != "/dashboard/plans" {
		t.Fatalf("title %q body %q link %q", d.Title, d.Body, d.ActionURL)
	}
	if d.DedupKey != "plan_expiry_reminder:ws-1:"+strconv.FormatInt(end.Unix(), 10) {
		t.Fatalf("dedup = %q, one reminder per billing period", d.DedupKey)
	}
	email := d.EmailCopy
	if email == nil || email.Template != "plan_expiry_reminder.html" || email.Placeholders["PlanName"] != "Plano Growth" ||
		email.Placeholders["ExpiryDate"] != "24/10/2026" || email.Placeholders["DashboardURL"] != "https://app.vozko.test/dashboard/plans" {
		t.Fatalf("email = %+v", email)
	}
}

func TestAFailedReminderIsNotCountedAsSent(t *testing.T) {
	publisher := &notificationtest.Publisher{Err: errors.New("db down")}
	uc := NewRemindExpiringSubscriptionsUseCase(
		upcomingExpirations{{WorkspaceID: "ws-1", PlanDefinitionID: "missing", CurrentPeriodEnd: time.Now()}},
		namedPlans{}, publisher, "https://app.vozko.test",
	)
	if sent, _ := uc.RemindExpiring(); sent != 0 {
		t.Fatalf("sent = %d", sent)
	}
	if drafts := publisher.Drafts(); len(drafts) != 1 || !strings.Contains(drafts[0].Title, "seu plano") {
		t.Fatalf("an unknown plan is named generically: %+v", drafts)
	}
}
