package emailsending

import (
	"context"
	"fmt"
	"log"
	"time"

	es "vozko/domain/emailsending"
)

const (
	quotaAlertKey         = "email:ses:send_quota:alerted"
	quotaAlertRepeat      = 6 * time.Hour
	quotaReadFailedKey    = "email:ses:send_quota:read_failed"
	quotaReadFailedRepeat = time.Hour
)

type SendQuotaRefresh struct {
	c *core
}

func (r *SendQuotaRefresh) Execute(ctx context.Context) error {
	c := r.c
	readAt := c.Now()
	quota, err := c.Quota.ReadSendQuota(es.WithProviderLane(ctx, es.LaneAccount))
	if err != nil {
		c.alertOncePer(ctx, quotaReadFailedKey, quotaReadFailedRepeat, "Cota de envio do SES não lida",
			fmt.Sprintf("o ritmo de envio segue a última leitura; campanhas param se ela passar de %s: %v", es.SendQuotaStaleAfter, err))
		return providerFailure("send quota", err)
	}
	quota.Read, quota.ReadAt = true, readAt
	if err := saveSendQuota(c.Shared, quota); err != nil {
		return err
	}
	c.clearAlert(quotaReadFailedKey)
	if !quota.NearlyExhausted(0) {
		c.clearAlert(quotaAlertKey)
		return nil
	}
	c.alertOncePer(ctx, quotaAlertKey, quotaAlertRepeat, "Cota de 24 horas do SES acima de 80%",
		fmt.Sprintf("%d de %d envios nas últimas 24 horas (%s); campanhas param ao chegar a %d e os %d restantes ficam para respostas, transacionais e testes",
			quota.Used24Hours(0), quota.DailyQuota(), basisPointsAsPercent(quota.UsageBasisPoints(0)), quota.DailyQuota()-quota.PriorityReserve(), quota.PriorityReserve()))
	return nil
}

type SendQuotaView struct {
	Quota            es.SendQuota
	SentSinceRead    int64
	Used24Hours      int64
	UsageBasisPoints int64
	PriorityReserve  int64
	Fresh            bool
}

func (a *Admin) SendQuota(context.Context) (SendQuotaView, error) {
	c := a.c
	now := c.Now()
	quota, err := loadSendQuota(c.Shared, now)
	if err != nil {
		return SendQuotaView{}, err
	}
	sent, err := sentSince(c.Shared, quota.ReadAt, now)
	if err != nil {
		return SendQuotaView{}, err
	}
	return SendQuotaView{
		Quota:            quota,
		SentSinceRead:    sent,
		Used24Hours:      quota.Used24Hours(sent),
		UsageBasisPoints: quota.UsageBasisPoints(sent),
		PriorityReserve:  quota.PriorityReserve(),
		Fresh:            quota.Fresh(now),
	}, nil
}

func (c *core) alertOncePer(ctx context.Context, key string, every time.Duration, subject, detail string) {
	first, err := c.Shared.SetNX(key, "1", every)
	if err != nil {
		log.Printf("[emailsending] alert episode %s unreadable, alerting anyway: %v", key, err)
	}
	if err == nil && !first {
		return
	}
	c.opsAlert(ctx, subject, detail)
}

func (c *core) clearAlert(key string) {
	if err := c.Shared.Del(key); err != nil {
		log.Printf("[emailsending] alert episode %s not cleared: %v", key, err)
	}
}

func basisPointsAsPercent(bp int64) string {
	return fmt.Sprintf("%d,%02d%%", bp/100, bp%100)
}
