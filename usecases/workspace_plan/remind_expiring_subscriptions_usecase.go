package workspace_plan_usecase

import (
	"strconv"
	"time"

	"vozko/brand"
	"vozko/domain/notification"
	workspace_plan "vozko/domain/workspace/workspace_plan"
)

const planExpiryReminderLeadTime = 7 * 24 * time.Hour

type upcomingExpirationsReader interface {
	ListUpcomingExpirations(from, to time.Time, batchSize int) ([]*workspace_plan.WorkspaceSubscription, error)
}

type remindExpiringSubscriptionsUseCase struct {
	subscriptions upcomingExpirationsReader
	plans         workspace_plan.PlanReader
	notifier      notification.Notifier
	dashboardURL  string
	batchSize     int
	now           clockFn
}

func NewRemindExpiringSubscriptionsUseCase(
	subscriptions upcomingExpirationsReader,
	plans workspace_plan.PlanReader,
	notifier notification.Notifier,
	dashboardURL string,
) workspace_plan.RemindExpiringSubscriptionsUseCase {
	return &remindExpiringSubscriptionsUseCase{
		subscriptions: subscriptions,
		plans:         plans,
		notifier:      notifier,
		dashboardURL:  dashboardURL,
		batchSize:     defaultExpireBatchSize,
		now:           utcNow,
	}
}

func (uc *remindExpiringSubscriptionsUseCase) RemindExpiring() (int, error) {
	if uc.notifier == nil {
		return 0, nil
	}
	now := uc.now()
	due, err := uc.subscriptions.ListUpcomingExpirations(now, now.Add(planExpiryReminderLeadTime), uc.batchSize)
	if err != nil {
		return 0, err
	}
	sent := 0
	for _, sub := range due {
		planName := "o seu plano"
		if p, perr := uc.plans.GetByID(sub.PlanDefinitionID); perr == nil && p != nil && p.Name != "" {
			planName = p.Name
		}
		if nerr := uc.notifier.Notify(notification.Notification{
			WorkspaceID: sub.WorkspaceID,
			Subject:     "Seu plano expira em breve - " + brand.Active().Name,
			Template:    "plan_expiry_reminder.html",
			Placeholders: map[string]interface{}{
				"PlanName":     planName,
				"ExpiryDate":   sub.CurrentPeriodEnd.Format("02/01/2006"),
				"DashboardURL": uc.dashboardURL,
			},
			DedupKey: "plan_expiry_reminder:" + sub.WorkspaceID + ":" + strconv.FormatInt(sub.CurrentPeriodEnd.Unix(), 10),
			DedupTTL: planExpiryReminderLeadTime + 24*time.Hour,
		}); nerr == nil {
			sent++
		}
	}
	return sent, nil
}
