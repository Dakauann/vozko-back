package calls_usecase

import (
	"log"
	"time"

	"vozko/domain/calls/billing"
)

type ReconcileOrphanedBillingUseCase struct {
	billingRepo billing.Repository
	logger      *log.Logger
}

func NewReconcileOrphanedBillingUseCase(
	billingRepo billing.Repository,
	logger *log.Logger,
) *ReconcileOrphanedBillingUseCase {
	return &ReconcileOrphanedBillingUseCase{
		billingRepo: billingRepo,
		logger:      logger,
	}
}

func (uc *ReconcileOrphanedBillingUseCase) Execute(threshold time.Duration) (int, error) {
	orphaned, err := uc.billingRepo.FindOrphaned(threshold)
	if err != nil {
		return 0, err
	}

	if len(orphaned) == 0 {
		return 0, nil
	}

	uc.logf("reconciliation: found %d orphaned billing records", len(orphaned))

	marked := 0
	for _, record := range orphaned {
		if err := uc.billingRepo.UpdateStatus(record.CallID, billing.StatusOrphaned); err != nil {
			uc.logf("reconciliation: failed to mark call %s as orphaned: %v", record.CallID, err)
			continue
		}
		uc.logf("reconciliation: marked call %s as orphaned (created %s ago)",
			record.CallID, time.Since(record.CreatedAt).Round(time.Second))
		marked++
	}

	return marked, nil
}

func (uc *ReconcileOrphanedBillingUseCase) RunPeriodic(interval, threshold time.Duration) {
	go func() {

		time.Sleep(2 * time.Minute)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			count, err := uc.Execute(threshold)
			if err != nil {
				uc.logf("reconciliation: periodic run failed: %v", err)
				continue
			}
			if count > 0 {
				uc.logf("reconciliation: periodic run marked %d orphaned records", count)
			}
		}
	}()
}

func (uc *ReconcileOrphanedBillingUseCase) logf(format string, args ...interface{}) {
	if uc.logger != nil {
		uc.logger.Printf(format, args...)
	}
}
