package emailapi_usecase

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"vozko/domain/balance"
	"vozko/domain/emailapi"
	"vozko/domain/notification"
)

type BalanceReader interface {
	GetByWorkspaceID(workspaceID string) (*balance.Balance, error)
}

type Quoter interface {
	QuoteSend(ctx context.Context, workspaceID string, recipients, encodedAttachmentBytes int64) (quote, error)
}

type ReserveWatch struct {
	WorkspaceID string
	Balances    BalanceReader
	Quotes      Quoter
	Alerts      notification.EpisodePublisher
}

func ReserveKey(workspaceID string) string { return "system_email_reserve:" + workspaceID }

func (w ReserveWatch) Execute(ctx context.Context) error {
	if w.WorkspaceID == "" || w.Balances == nil || w.Quotes == nil || w.Alerts == nil {
		return ErrIncomplete
	}
	q, err := w.Quotes.QuoteSend(ctx, w.WorkspaceID, emailapi.SystemEmailReserve, 0)
	if err != nil {
		return fmt.Errorf("emailapi: pricing the system email reserve: %w", err)
	}
	current, err := w.Balances.GetByWorkspaceID(w.WorkspaceID)
	if err != nil && !errors.Is(err, balance.ErrBalanceNotFound) {
		return fmt.Errorf("emailapi: reading the system wallet: %w", err)
	}
	var amount int64
	if current != nil {
		amount = current.Amount
	}
	if amount >= q.TotalMicros {
		return w.Alerts.Resolve(ctx, w.WorkspaceID, ReserveKey(w.WorkspaceID))
	}
	left := int64(0)
	if q.UnitMicros > 0 && amount > 0 {
		left = amount / q.UnitMicros
	}
	_, err = w.Alerts.Publish(ctx, notification.Draft{
		WorkspaceID: w.WorkspaceID,
		Category:    notification.CategoryBilling,
		Severity:    notification.SeverityCritical,
		Title:       "Saldo dos e-mails do sistema está acabando",
		Body:        "A carteira que paga os e-mails do sistema (redefinição de senha, convites, avisos) cobre cerca de " + strconv.FormatInt(left, 10) + " e-mails. Quando acabar, esses e-mails param de sair. Recarregue a carteira deste workspace.",
		ActionURL:   "/dashboard/balance",
		ActionLabel: "Recarregar carteira",
		Audience:    notification.Managers(),
		Email:       true,
		EpisodeKey:  ReserveKey(w.WorkspaceID),
		Source:      notification.SourceSystemEmail,
	})
	return err
}
