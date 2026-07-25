package cron

import (
	"vozko/domain/cron"
	"vozko/domain/order"
)

type OrderCleanupJob struct {
	cancelOrderUseCase order.CancelExpiredOrderUseCase
}

func NewOrderCleanupJob(cancelOrderUseCase order.CancelExpiredOrderUseCase) cron.OrderCleanupChecker {
	return &OrderCleanupJob{
		cancelOrderUseCase: cancelOrderUseCase,
	}
}

func (j *OrderCleanupJob) CheckExpiredOrders() error {
	return j.cancelOrderUseCase.Execute()
}
