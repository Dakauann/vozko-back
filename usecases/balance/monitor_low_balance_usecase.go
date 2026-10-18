package balance_usecase

import (
	"context"
	"fmt"
	"log"
	"strings"

	"vozko/brand"
	"vozko/domain/balance"
	"vozko/domain/cache"
	"vozko/domain/notification"
	"vozko/domain/workspace"
)

const (
	defaultLowBalanceThresholdMicros int64  = 3_000_000
	lowBalanceAlertedSetKey          string = "low_balance:alerted"
	lowBalancePath                          = "/dashboard/balance"
)

type MonitorLowBalanceUseCase struct {
	lister          balance.LowBalanceLister
	publisher       notification.Publisher
	state           cache.SharedState
	dashboardURL    string
	thresholdMicros int64
}

func NewMonitorLowBalanceUseCase(lister balance.LowBalanceLister, publisher notification.Publisher, state cache.SharedState, dashboardURL string, thresholdMicros int64) *MonitorLowBalanceUseCase {
	if thresholdMicros <= 0 {
		thresholdMicros = defaultLowBalanceThresholdMicros
	}
	return &MonitorLowBalanceUseCase{
		lister:          lister,
		publisher:       publisher,
		state:           state,
		dashboardURL:    strings.TrimRight(dashboardURL, "/"),
		thresholdMicros: thresholdMicros,
	}
}

func (uc *MonitorLowBalanceUseCase) Run() (int, error) {
	if uc.lister == nil || uc.publisher == nil {
		return 0, nil
	}
	rows, err := uc.lister.ListWorkspacesBelowBalance(uc.thresholdMicros)
	if err != nil {
		return 0, err
	}

	belowSet := make(map[string]bool, len(rows))
	for _, r := range rows {
		belowSet[r.WorkspaceID] = true
	}
	alerted := uc.previouslyAlerted()

	for _, r := range rows {
		if alerted[r.WorkspaceID] {
			continue
		}
		if err := uc.notify(r); err != nil {
			log.Printf("[balance-monitor] low balance notification for %s failed, retrying next run: %v", r.WorkspaceID, err)
			continue
		}
		uc.markAlerted(r.WorkspaceID)
	}

	for ws := range alerted {
		if !belowSet[ws] {
			uc.clearAlerted(ws)
		}
	}

	return len(rows), nil
}

func (uc *MonitorLowBalanceUseCase) notify(row balance.LowBalanceRow) error {
	amount := formatWalletAmount(row.AmountMicros, row.Currency)
	_, err := uc.publisher.Publish(context.Background(), notification.Draft{
		WorkspaceID: row.WorkspaceID,
		Category:    notification.CategoryBilling,
		Severity:    notification.SeverityWarning,
		Title:       "Saldo baixo",
		Body:        "O saldo da carteira está em " + amount + ". Ao chegar a zero, as respostas de IA e novas mensagens podem ser pausadas. Recarregue para não interromper o atendimento.",
		ActionURL:   lowBalancePath,
		ActionLabel: "Recarregar saldo",
		Audience:    notification.Holding(workspace.ResourceBalance, workspace.ActionRead),
		Email:       true,
		EmailCopy: &notification.EmailCopy{
			Template: "low_balance_warning.html",
			Subject:  "Saldo baixo - " + brand.Active().Name,
			Placeholders: map[string]interface{}{
				"Balance":      amount,
				"DashboardURL": notification.AbsoluteLink(uc.dashboardURL, lowBalancePath),
			},
		},
		Source: notification.SourceLowBalance,
	})
	return err
}
func (uc *MonitorLowBalanceUseCase) previouslyAlerted() map[string]bool {
	set := map[string]bool{}
	if uc.state == nil {
		return set
	}
	members, err := uc.state.SMembers(lowBalanceAlertedSetKey)
	if err != nil {
		log.Printf("[balance-monitor] read alerted set failed: %v", err)
		return set
	}
	for _, m := range members {
		set[m] = true
	}
	return set
}

func (uc *MonitorLowBalanceUseCase) markAlerted(workspaceID string) {
	if uc.state == nil {
		return
	}
	if err := uc.state.SAdd(lowBalanceAlertedSetKey, workspaceID); err != nil {
		log.Printf("[balance-monitor] mark alerted %s failed: %v", workspaceID, err)
	}
}

func (uc *MonitorLowBalanceUseCase) clearAlerted(workspaceID string) {
	if uc.state == nil {
		return
	}
	if err := uc.state.SRem(lowBalanceAlertedSetKey, workspaceID); err != nil {
		log.Printf("[balance-monitor] clear alerted %s failed: %v", workspaceID, err)
	}
}

func formatWalletAmount(micros int64, currency string) string {
	amount := fmt.Sprintf("%.2f", float64(micros)/1_000_000)
	amount = strings.Replace(amount, ".", ",", 1)
	prefix := "US$"
	if strings.EqualFold(currency, "BRL") {
		prefix = "R$"
	}
	return prefix + " " + amount
}
