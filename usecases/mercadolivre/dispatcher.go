package mercadolivre

import (
	"context"
	"errors"
	"fmt"

	mldomain "vozko/domain/mercadolivre"
)

type NotificationDispatcher struct {
	accounts mldomain.AccountRepository
	metrics  Metrics
	handlers map[mldomain.JobKind]JobHandler
}

func NewNotificationDispatcher(accounts mldomain.AccountRepository, metrics Metrics, handlers map[mldomain.JobKind]JobHandler) *NotificationDispatcher {
	if accounts == nil || metrics == nil {
		panic("mercadolivre: the notification dispatcher requires accounts and metrics")
	}
	registered := make(map[mldomain.JobKind]JobHandler, len(handlers))
	for kind, h := range handlers {
		if h == nil {
			panic("mercadolivre: nil handler for job kind " + string(kind))
		}
		registered[kind] = h
	}
	return &NotificationDispatcher{accounts: accounts, metrics: metrics, handlers: registered}
}

func (d *NotificationDispatcher) Execute(ctx context.Context, job *mldomain.Job) error {
	if job == nil {
		return mldomain.ErrInvalidNotification
	}
	handler, ok := d.handlers[job.Kind]
	if !ok {
		d.metrics.AddMercadoLivreEvent("consume", "kind_not_handled")
		return fmt.Errorf("%w: %s", ErrJobKindNotHandled, job.Kind)
	}
	account, err := d.accounts.FindLiveByMLUserID(ctx, job.SellerID)
	if errors.Is(err, mldomain.ErrAccountNotFound) {
		d.metrics.AddMercadoLivreEvent("consume", "unknown_account")
		return ErrUnknownAccount
	}
	if err != nil {
		return err
	}
	if account.Status != mldomain.StatusConnected {
		d.metrics.AddMercadoLivreEvent("consume", "account_unavailable")
		return fmt.Errorf("%w: %s", ErrAccountUnavailable, account.Status)
	}
	if job.Marker != "" {
		ctx = mldomain.WithBackgroundPriority(ctx)
	}
	return handler.Handle(ctx, account, *job)
}
