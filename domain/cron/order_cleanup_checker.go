package cron

type OrderCleanupChecker interface {
	CheckExpiredOrders() error
}
