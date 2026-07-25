package dialer_usecase

import (
	"context"
	"fmt"
	"log"
	"time"

	"vozko/domain/balance"
	config_domain "vozko/domain/config"
	"vozko/domain/dialer"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

type callAdmissionCoordinator struct {
	cachedBalanceChecker balance.CachedBalanceChecker
	pricer               workspace_pricing.Pricer
	inflightReserver     balance.InflightReserver
	callSlotGate         dialer.CallSlotGate
	systemConfigRepo     config_domain.SystemConfigRepository
	logger               *log.Logger
}

func NewCallAdmissionCoordinator(
	cachedBalanceChecker balance.CachedBalanceChecker,
	pricer workspace_pricing.Pricer,
	inflightReserver balance.InflightReserver,
	callSlotGate dialer.CallSlotGate,
	systemConfigRepo config_domain.SystemConfigRepository,
	logger *log.Logger,
) dialer.CallAdmissionCoordinator {
	if logger == nil {
		logger = log.Default()
	}
	return &callAdmissionCoordinator{
		cachedBalanceChecker: cachedBalanceChecker,
		pricer:               pricer,
		inflightReserver:     inflightReserver,
		callSlotGate:         callSlotGate,
		systemConfigRepo:     systemConfigRepo,
		logger:               logger,
	}
}

func (c *callAdmissionCoordinator) Acquire(ctx context.Context, input dialer.CallAdmissionInput) (*dialer.CallAdmissionLease, error) {
	if input.WorkspaceID == "" {
		return nil, dialer.ErrWorkspaceRequired
	}
	if c.cachedBalanceChecker == nil || c.pricer == nil || c.inflightReserver == nil {
		return nil, dialer.ErrAdmissionDependenciesMissing
	}

	pr, err := c.pricer.PriceTelephonyChannel(input.WorkspaceID, 60.0, input.CallChannel)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", dialer.ErrTelephonyPricingUnavailable, err)
	}

	lease := &dialer.CallAdmissionLease{
		WorkspaceID:         input.WorkspaceID,
		PerMinuteCostMicros: pr.PriceMicros,
		AcquiredAt:          time.Now(),
	}

	if err := c.acquireSlot(ctx, input, lease); err != nil {
		return nil, err
	}
	if err := c.reserveInflight(input, lease); err != nil {
		if lease.SlotAcquired && c.callSlotGate != nil {
			c.callSlotGate.Release(lease.WorkspaceID)
			lease.SlotAcquired = false
		}
		return nil, err
	}
	return lease, nil
}

func (c *callAdmissionCoordinator) Refresh(lease *dialer.CallAdmissionLease, ttl time.Duration) error {
	if lease == nil || lease.WorkspaceID == "" || lease.ReservedMicros <= 0 || ttl <= 0 {
		return nil
	}
	if err := c.inflightReserver.RefreshTTL(lease.WorkspaceID, ttl); err != nil {
		return fmt.Errorf("%w: %v", dialer.ErrReservationFailed, err)
	}
	return nil
}

func (c *callAdmissionCoordinator) Release(lease *dialer.CallAdmissionLease) error {
	if lease == nil {
		return nil
	}
	var releaseErr error
	if lease.ReservedMicros > 0 && c.inflightReserver != nil {
		if err := c.inflightReserver.Release(lease.WorkspaceID, lease.ReservedMicros); err != nil {
			releaseErr = err
		}
	}
	if lease.SlotAcquired && c.callSlotGate != nil {
		c.callSlotGate.Release(lease.WorkspaceID)
	}
	lease.ReservedMicros = 0
	lease.SlotAcquired = false
	if releaseErr != nil {
		return fmt.Errorf("%w: %v", dialer.ErrReservationFailed, releaseErr)
	}
	return nil
}

func (c *callAdmissionCoordinator) acquireSlot(ctx context.Context, input dialer.CallAdmissionInput, lease *dialer.CallAdmissionLease) error {
	if c.callSlotGate == nil {
		return nil
	}
	globalMax := input.GlobalMax
	if globalMax <= 0 {
		globalMax = int64(config_domain.DefaultMaxConcurrentCalls)
		if c.systemConfigRepo != nil {
			if cfg, err := c.systemConfigRepo.Get(context.Background()); err == nil {
				globalMax = int64(cfg.GetMaxConcurrentCalls())
			}
		}
	}
	if globalMax <= 0 {
		return dialer.ErrNoCallSlotsAvailable
	}

	pollInterval := input.SlotPollInterval
	if pollInterval <= 0 {
		pollInterval = time.Second
	}
	timeout := input.SlotPollTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	if _, ok := c.callSlotGate.Acquire(input.WorkspaceID, globalMax); ok {
		lease.SlotAcquired = true
		return nil
	}
	if input.OnWaitingForSlot != nil {
		input.OnWaitingForSlot()
	}

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return dialer.ErrNoCallSlotsAvailable
		case <-ticker.C:
			if _, ok := c.callSlotGate.Acquire(input.WorkspaceID, globalMax); ok {
				lease.SlotAcquired = true
				return nil
			}
		}
	}
}

func (c *callAdmissionCoordinator) reserveInflight(input dialer.CallAdmissionInput, lease *dialer.CallAdmissionLease) error {
	if lease == nil || lease.PerMinuteCostMicros <= 0 {
		return nil
	}
	budget, err := c.cachedBalanceChecker.GetBalance(input.WorkspaceID)
	if err != nil {
		return fmt.Errorf("%w: %v", dialer.ErrBalanceCheckFailed, err)
	}
	ok, err := c.inflightReserver.Reserve(input.WorkspaceID, lease.PerMinuteCostMicros, budget)
	if err != nil {
		return fmt.Errorf("%w: %v", dialer.ErrReservationFailed, err)
	}
	if !ok {
		return dialer.ErrInsufficientBalance
	}
	lease.ReservedMicros = lease.PerMinuteCostMicros
	ttl := input.ReservationTTL
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	if err := c.Refresh(lease, ttl); err != nil {
		c.logger.Printf("dialer admission: failed to refresh inflight ttl for workspace %s: %v", input.WorkspaceID, err)
	}
	return nil
}
