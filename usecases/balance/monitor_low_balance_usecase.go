package balance_usecase

import (
	"fmt"
	"log"
	"strings"

	"vozko/brand"
	"vozko/domain/balance"
	"vozko/domain/cache"
	"vozko/domain/notification"
)

const (
	defaultLowBalanceThresholdMicros int64  = 3_000_000
	lowBalanceAlertedSetKey          string = "low_balance:alerted"
)

type MonitorLowBalanceUseCase struct {
	lister          balance.LowBalanceLister
	notifier        notification.Notifier
	state           cache.SharedState
	dashboardURL    string
	thresholdMicros int64
}

func NewMonitorLowBalanceUseCase(lister balance.LowBalanceLister, notifier notification.Notifier, state cache.SharedState, dashboardURL string, thresholdMicros int64) *MonitorLowBalanceUseCase {
	if thresholdMicros <= 0 {
		thresholdMicros = defaultLowBalanceThresholdMicros
	}
	return &MonitorLowBalanceUseCase{
		lister:          lister,
		notifier:        notifier,
		state:           state,
		dashboardURL:    dashboardURL,
		thresholdMicros: thresholdMicros,
	}
}

func (uc *MonitorLowBalanceUseCase) Run() (int, error) {
	if uc.lister == nil || uc.notifier == nil {
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
		uc.notify(r)
		uc.markAlerted(r.WorkspaceID)
	}

	for ws := range alerted {
		if !belowSet[ws] {
			uc.clearAlerted(ws)
		}
	}

	return len(rows), nil
}

func (uc *MonitorLowBalanceUseCase) notify(row balance.LowBalanceRow) {
	_ = uc.notifier.Notify(notification.Notification{
		WorkspaceID: row.WorkspaceID,
		Subject:     "Saldo baixo - " + brand.Active().Name,
		Template:    "low_balance_warning.html",
		Placeholders: map[string]interface{}{
			"Balance":      formatWalletAmount(row.AmountMicros, row.Currency),
			"DashboardURL": uc.dashboardURL,
		},
	})
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
