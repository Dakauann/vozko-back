package workspace_plan_usecase

import (
	"context"
	"log"
	"strconv"
	"strings"
	"time"

	"vozko/brand"
	"vozko/domain/notification"
	"vozko/domain/workspace"
	workspace_plan "vozko/domain/workspace/workspace_plan"
)

const (
	planExpiryReminderLeadTime = 7 * 24 * time.Hour
	plansPath                  = "/dashboard/plans"
)

type upcomingExpirationsReader interface {
	ListUpcomingExpirations(from, to time.Time, batchSize int) ([]*workspace_plan.WorkspaceSubscription, error)
}

type remindExpiringSubscriptionsUseCase struct {
	subscriptions upcomingExpirationsReader
	plans         workspace_plan.PlanReader
	publisher     notification.Publisher
	dashboardURL  string
	batchSize     int
	now           clockFn
}

func NewRemindExpiringSubscriptionsUseCase(
	subscriptions upcomingExpirationsReader,
	plans workspace_plan.PlanReader,
	publisher notification.Publisher,
	dashboardURL string,
) workspace_plan.RemindExpiringSubscriptionsUseCase {
	return &remindExpiringSubscriptionsUseCase{
		subscriptions: subscriptions,
		plans:         plans,
		publisher:     publisher,
		dashboardURL:  strings.TrimRight(dashboardURL, "/"),
		batchSize:     defaultExpireBatchSize,
		now:           utcNow,
	}
}

func (uc *remindExpiringSubscriptionsUseCase) RemindExpiring() (int, error) {
	if uc.publisher == nil {
		return 0, nil
	}
	now := uc.now()
	due, err := uc.subscriptions.ListUpcomingExpirations(now, now.Add(planExpiryReminderLeadTime), uc.batchSize)
	if err != nil {
		return 0, err
	}
	sent := 0
	for _, sub := range due {
		if err := uc.remind(sub); err != nil {
			log.Printf("[plan-expiry] reminder for workspace %s failed: %v", sub.WorkspaceID, err)
			continue
		}
		sent++
	}
	return sent, nil
}

func (uc *remindExpiringSubscriptionsUseCase) remind(sub *workspace_plan.WorkspaceSubscription) error {
	planName := "o seu plano"
	if p, err := uc.plans.GetByID(sub.PlanDefinitionID); err == nil && p != nil && p.Name != "" {
		planName = p.Name
	}
	expiry := sub.CurrentPeriodEnd.Format("02/01/2006")
	_, err := uc.publisher.Publish(context.Background(), notification.Draft{
		WorkspaceID: sub.WorkspaceID,
		Category:    notification.CategoryBilling,
		Severity:    notification.SeverityWarning,
		Title:       notification.ClipTitle("Renove " + planName + " até " + expiry),
		Body:        "O plano " + planName + " expira em " + expiry + ". Renove para não interromper as chamadas, automações e números de WhatsApp incluídos no plano.",
		ActionURL:   plansPath,
		ActionLabel: "Renovar plano",
		Audience:    notification.Holding(workspace.ResourcePlans, workspace.ActionCreate),
		Email:       true,
		EmailCopy: &notification.EmailCopy{
			Template: "plan_expiry_reminder.html",
			Subject:  "Seu plano expira em breve - " + brand.Active().Name,
			Placeholders: map[string]interface{}{
				"PlanName":     planName,
				"ExpiryDate":   expiry,
				"DashboardURL": notification.AbsoluteLink(uc.dashboardURL, plansPath),
			},
		},
		DedupKey: "plan_expiry_reminder:" + sub.WorkspaceID + ":" + strconv.FormatInt(sub.CurrentPeriodEnd.Unix(), 10),
		Source:   notification.SourcePlanExpiry,
	})
	return err
}
