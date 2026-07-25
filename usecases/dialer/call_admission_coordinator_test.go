package dialer_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/balance"
	config_domain "vozko/domain/config"
	"vozko/domain/dialer"
	workspace_domain "vozko/domain/workspace"
	workspace_pricing "vozko/domain/workspace/workspace_pricing"
)

type stubCachedBalanceChecker struct {
	balance int64
	err     error
}

func (s *stubCachedBalanceChecker) HasSufficientBalance(_ string, amountMicros int64) (bool, error) {
	if s.err != nil {
		return false, s.err
	}
	return s.balance >= amountMicros, nil
}

func (s *stubCachedBalanceChecker) GetBalance(_ string) (int64, error) {
	if s.err != nil {
		return 0, s.err
	}
	return s.balance, nil
}

func (s *stubCachedBalanceChecker) Invalidate(_ string)          {}
func (s *stubCachedBalanceChecker) InvalidateDebounced(_ string) {}

type stubInflightReserver struct {
	reserveOK    bool
	reserveErr   error
	releaseErr   error
	refreshErr   error
	released     []int64
	refreshCalls int
}

func (s *stubInflightReserver) Reserve(_ string, deltaMicros int64, _ int64) (bool, error) {
	if s.reserveErr != nil {
		return false, s.reserveErr
	}
	if !s.reserveOK {
		return false, nil
	}
	return true, nil
}

func (s *stubInflightReserver) Release(_ string, deltaMicros int64) error {
	s.released = append(s.released, deltaMicros)
	return s.releaseErr
}

func (s *stubInflightReserver) RefreshTTL(_ string, _ time.Duration) error {
	s.refreshCalls++
	return s.refreshErr
}

func (s *stubInflightReserver) GetInflight(_ string) (int64, error) {
	return 0, nil
}

type testSlotGate struct {
	results      []bool
	acquireCalls int
	releaseCalls int
}

func (s *testSlotGate) Acquire(_ string, _ int64) (workspace_domain.AcquireResult, bool) {
	s.acquireCalls++
	if len(s.results) == 0 {
		return workspace_domain.AcquireResult{}, false
	}
	result := s.results[0]
	s.results = s.results[1:]
	return workspace_domain.AcquireResult{}, result
}

func (s *testSlotGate) Release(_ string) {
	s.releaseCalls++
}

type stubPricer struct {
	telephony workspace_pricing.PriceResult
	err       error
}

func (s *stubPricer) ResolveForWorkspace(string) ([]workspace_pricing.ResolvedPricingItem, error) {
	return nil, nil
}
func (s *stubPricer) PriceSTT(string, string, float64) (workspace_pricing.PriceResult, error) {
	return workspace_pricing.PriceResult{}, nil
}
func (s *stubPricer) PriceTTS(string, string, string, int) (workspace_pricing.PriceResult, error) {
	return workspace_pricing.PriceResult{}, nil
}
func (s *stubPricer) PriceLLM(string, string, int, int) (workspace_pricing.PriceResult, error) {
	return workspace_pricing.PriceResult{}, nil
}
func (s *stubPricer) PriceTelephony(string, float64) (workspace_pricing.PriceResult, error) {
	if s.err != nil {
		return workspace_pricing.PriceResult{}, s.err
	}
	return s.telephony, nil
}
func (s *stubPricer) PriceTelephonyChannel(ws string, dur float64, _ string) (workspace_pricing.PriceResult, error) {
	return s.PriceTelephony(ws, dur)
}
func (s *stubPricer) PriceWhatsApp(string, string) (workspace_pricing.PriceResult, error) {
	return workspace_pricing.PriceResult{}, nil
}
func TestCallAdmissionCoordinatorAcquireSuccess(t *testing.T) {
	checker := &stubCachedBalanceChecker{balance: 100_000}
	inflight := &stubInflightReserver{reserveOK: true}
	slotGate := &testSlotGate{results: []bool{true}}
	pricer := &stubPricer{telephony: workspace_pricing.PriceResult{PriceMicros: 50_000}}
	coordinator := NewCallAdmissionCoordinator(checker, pricer, inflight, slotGate, nil, nil)

	lease, err := coordinator.Acquire(context.Background(), dialer.CallAdmissionInput{WorkspaceID: "ws-1"})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if lease == nil {
		t.Fatal("Acquire() lease = nil")
	}
	if !lease.SlotAcquired {
		t.Fatal("expected slot to be acquired")
	}
	if lease.ReservedMicros != 50_000 {
		t.Fatalf("ReservedMicros = %d, want 50000", lease.ReservedMicros)
	}
	if inflight.refreshCalls != 1 {
		t.Fatalf("RefreshTTL calls = %d, want 1", inflight.refreshCalls)
	}

	if err := coordinator.Release(lease); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	if len(inflight.released) != 1 || inflight.released[0] != 50_000 {
		t.Fatalf("released = %#v, want [50000]", inflight.released)
	}
	if slotGate.releaseCalls != 1 {
		t.Fatalf("slot release calls = %d, want 1", slotGate.releaseCalls)
	}
}

func TestCallAdmissionCoordinatorAcquireInsufficientBalanceReleasesSlot(t *testing.T) {
	checker := &stubCachedBalanceChecker{balance: 10_000}
	inflight := &stubInflightReserver{reserveOK: false}
	slotGate := &testSlotGate{results: []bool{true}}
	pricer := &stubPricer{telephony: workspace_pricing.PriceResult{PriceMicros: 50_000}}
	coordinator := NewCallAdmissionCoordinator(checker, pricer, inflight, slotGate, nil, nil)

	lease, err := coordinator.Acquire(context.Background(), dialer.CallAdmissionInput{WorkspaceID: "ws-1"})
	if !errors.Is(err, dialer.ErrInsufficientBalance) {
		t.Fatalf("Acquire() error = %v, want ErrInsufficientBalance", err)
	}
	if lease != nil {
		t.Fatalf("Acquire() lease = %#v, want nil", lease)
	}
	if slotGate.releaseCalls != 1 {
		t.Fatalf("slot release calls = %d, want 1", slotGate.releaseCalls)
	}
	if len(inflight.released) != 0 {
		t.Fatalf("unexpected inflight release calls: %#v", inflight.released)
	}
}

func TestCallAdmissionCoordinatorAcquireTimeout(t *testing.T) {
	checker := &stubCachedBalanceChecker{balance: 100_000}
	inflight := &stubInflightReserver{reserveOK: true}
	slotGate := &testSlotGate{results: []bool{false, false, false}}
	pricer := &stubPricer{telephony: workspace_pricing.PriceResult{PriceMicros: 50_000}}
	coordinator := NewCallAdmissionCoordinator(checker, pricer, inflight, slotGate, nil, nil)

	lease, err := coordinator.Acquire(context.Background(), dialer.CallAdmissionInput{
		WorkspaceID:      "ws-1",
		SlotPollInterval: 5 * time.Millisecond,
		SlotPollTimeout:  12 * time.Millisecond,
	})
	if !errors.Is(err, dialer.ErrNoCallSlotsAvailable) {
		t.Fatalf("Acquire() error = %v, want ErrNoCallSlotsAvailable", err)
	}
	if lease != nil {
		t.Fatalf("Acquire() lease = %#v, want nil", lease)
	}
	if inflight.refreshCalls != 0 {
		t.Fatalf("unexpected RefreshTTL calls = %d", inflight.refreshCalls)
	}
	if slotGate.releaseCalls != 0 {
		t.Fatalf("unexpected slot release calls = %d", slotGate.releaseCalls)
	}
}

var _ balance.CachedBalanceChecker = (*stubCachedBalanceChecker)(nil)
var _ balance.InflightReserver = (*stubInflightReserver)(nil)
var _ dialer.CallSlotGate = (*testSlotGate)(nil)
var _ workspace_pricing.Pricer = (*stubPricer)(nil)

type stubSystemConfigRepo struct {
	maxConcurrentCalls int
	getErr             error
}

func (s *stubSystemConfigRepo) Get(_ context.Context) (*config_domain.SystemConfig, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return &config_domain.SystemConfig{MaxConcurrentCalls: s.maxConcurrentCalls}, nil
}

func (s *stubSystemConfigRepo) Upsert(_ context.Context, _ *config_domain.SystemConfig) error {
	return nil
}

var _ config_domain.SystemConfigRepository = (*stubSystemConfigRepo)(nil)

func TestCallAdmissionCoordinatorAcquireEmptyWorkspace(t *testing.T) {
	coordinator := NewCallAdmissionCoordinator(
		&stubCachedBalanceChecker{balance: 100_000},
		&stubPricer{telephony: workspace_pricing.PriceResult{PriceMicros: 50_000}},
		&stubInflightReserver{reserveOK: true},
		&testSlotGate{results: []bool{true}},
		nil,
		nil,
	)

	lease, err := coordinator.Acquire(context.Background(), dialer.CallAdmissionInput{})
	if !errors.Is(err, dialer.ErrWorkspaceRequired) {
		t.Fatalf("Acquire() error = %v, want ErrWorkspaceRequired", err)
	}
	if lease != nil {
		t.Fatalf("Acquire() lease = %#v, want nil", lease)
	}
}

func TestCallAdmissionCoordinatorAcquireMissingDeps(t *testing.T) {
	testCases := []struct {
		name     string
		checker  balance.CachedBalanceChecker
		pricer   workspace_pricing.Pricer
		inflight balance.InflightReserver
	}{
		{"nil checker", nil, &stubPricer{}, &stubInflightReserver{}},
		{"nil pricer", &stubCachedBalanceChecker{}, nil, &stubInflightReserver{}},
		{"nil inflight", &stubCachedBalanceChecker{}, &stubPricer{}, nil},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			coordinator := NewCallAdmissionCoordinator(tc.checker, tc.pricer, tc.inflight, &testSlotGate{results: []bool{true}}, nil, nil)
			lease, err := coordinator.Acquire(context.Background(), dialer.CallAdmissionInput{WorkspaceID: "ws-1"})
			if !errors.Is(err, dialer.ErrAdmissionDependenciesMissing) {
				t.Fatalf("Acquire() error = %v, want ErrAdmissionDependenciesMissing", err)
			}
			if lease != nil {
				t.Fatalf("Acquire() lease = %#v, want nil", lease)
			}
		})
	}
}

func TestCallAdmissionCoordinatorAcquirePricerError(t *testing.T) {
	slotGate := &testSlotGate{results: []bool{true}}
	inflight := &stubInflightReserver{reserveOK: true}
	coordinator := NewCallAdmissionCoordinator(
		&stubCachedBalanceChecker{balance: 100_000},
		&stubPricer{err: errors.New("rate card missing")},
		inflight,
		slotGate,
		nil,
		nil,
	)

	lease, err := coordinator.Acquire(context.Background(), dialer.CallAdmissionInput{WorkspaceID: "ws-1"})
	if !errors.Is(err, dialer.ErrTelephonyPricingUnavailable) {
		t.Fatalf("Acquire() error = %v, want ErrTelephonyPricingUnavailable", err)
	}
	if lease != nil {
		t.Fatalf("Acquire() lease = %#v, want nil", lease)
	}
	if slotGate.acquireCalls != 0 {
		t.Fatalf("slot should not be acquired on pricer error, acquireCalls = %d", slotGate.acquireCalls)
	}
	if len(inflight.released) != 0 {
		t.Fatalf("inflight must not be touched on pricer error")
	}
}

func TestCallAdmissionCoordinatorAcquireUsesSystemConfigGlobalMax(t *testing.T) {
	slotGate := &testSlotGate{results: []bool{true}}
	inflight := &stubInflightReserver{reserveOK: true}
	configRepo := &stubSystemConfigRepo{maxConcurrentCalls: 12}
	coordinator := NewCallAdmissionCoordinator(
		&stubCachedBalanceChecker{balance: 100_000},
		&stubPricer{telephony: workspace_pricing.PriceResult{PriceMicros: 1_000}},
		inflight,
		slotGate,
		configRepo,
		nil,
	)

	lease, err := coordinator.Acquire(context.Background(), dialer.CallAdmissionInput{WorkspaceID: "ws-1"})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if !lease.SlotAcquired {
		t.Fatalf("expected slot acquisition using config-driven global max")
	}
}

func TestCallAdmissionCoordinatorAcquireSystemConfigReadError(t *testing.T) {
	slotGate := &testSlotGate{results: []bool{true}}
	inflight := &stubInflightReserver{reserveOK: true}
	configRepo := &stubSystemConfigRepo{getErr: errors.New("redis down")}
	coordinator := NewCallAdmissionCoordinator(
		&stubCachedBalanceChecker{balance: 100_000},
		&stubPricer{telephony: workspace_pricing.PriceResult{PriceMicros: 1_000}},
		inflight,
		slotGate,
		configRepo,
		nil,
	)

	lease, err := coordinator.Acquire(context.Background(), dialer.CallAdmissionInput{WorkspaceID: "ws-1"})
	if err != nil {
		t.Fatalf("Acquire() error = %v (expected fallback to default, not a hard fail)", err)
	}
	if !lease.SlotAcquired {
		t.Fatalf("expected slot acquired on default fallback")
	}
}

func TestCallAdmissionCoordinatorAcquireNilSlotGate(t *testing.T) {
	inflight := &stubInflightReserver{reserveOK: true}
	coordinator := NewCallAdmissionCoordinator(
		&stubCachedBalanceChecker{balance: 100_000},
		&stubPricer{telephony: workspace_pricing.PriceResult{PriceMicros: 50_000}},
		inflight,
		nil,
		nil,
		nil,
	)

	lease, err := coordinator.Acquire(context.Background(), dialer.CallAdmissionInput{WorkspaceID: "ws-1"})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if lease.SlotAcquired {
		t.Fatalf("SlotAcquired should be false when slot gate is nil")
	}
	if lease.ReservedMicros != 50_000 {
		t.Fatalf("ReservedMicros = %d, want 50000", lease.ReservedMicros)
	}
}

func TestCallAdmissionCoordinatorAcquireGlobalMaxZero(t *testing.T) {
	slotGate := &testSlotGate{results: []bool{true}}
	coordinator := NewCallAdmissionCoordinator(
		&stubCachedBalanceChecker{balance: 100_000},
		&stubPricer{telephony: workspace_pricing.PriceResult{PriceMicros: 50_000}},
		&stubInflightReserver{reserveOK: true},
		slotGate,
		nil,
		nil,
	)

	lease, err := coordinator.Acquire(context.Background(), dialer.CallAdmissionInput{
		WorkspaceID: "ws-1",
	})
	if err != nil {
		t.Fatalf("unexpected error on default-fallback path: %v", err)
	}
	if lease == nil || !lease.SlotAcquired {
		t.Fatalf("expected lease with acquired slot on default fallback")
	}
}

func TestCallAdmissionCoordinatorAcquireSlotWaitAndSucceed(t *testing.T) {
	waitingCalls := 0
	slotGate := &testSlotGate{results: []bool{false, false, true}}
	inflight := &stubInflightReserver{reserveOK: true}
	coordinator := NewCallAdmissionCoordinator(
		&stubCachedBalanceChecker{balance: 100_000},
		&stubPricer{telephony: workspace_pricing.PriceResult{PriceMicros: 10_000}},
		inflight,
		slotGate,
		nil,
		nil,
	)

	lease, err := coordinator.Acquire(context.Background(), dialer.CallAdmissionInput{
		WorkspaceID:      "ws-1",
		SlotPollInterval: 2 * time.Millisecond,
		SlotPollTimeout:  200 * time.Millisecond,
		OnWaitingForSlot: func() { waitingCalls++ },
	})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if !lease.SlotAcquired || lease.ReservedMicros != 10_000 {
		t.Fatalf("unexpected lease: %#v", lease)
	}
	if waitingCalls != 1 {
		t.Fatalf("OnWaitingForSlot should fire exactly once, got %d", waitingCalls)
	}
	if slotGate.acquireCalls < 3 {
		t.Fatalf("expected retry polling, acquireCalls = %d", slotGate.acquireCalls)
	}
}

func TestCallAdmissionCoordinatorAcquireContextCancelled(t *testing.T) {
	slotGate := &testSlotGate{results: []bool{false, false, false, false, false}}
	inflight := &stubInflightReserver{reserveOK: true}
	coordinator := NewCallAdmissionCoordinator(
		&stubCachedBalanceChecker{balance: 100_000},
		&stubPricer{telephony: workspace_pricing.PriceResult{PriceMicros: 10_000}},
		inflight,
		slotGate,
		nil,
		nil,
	)

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()

	lease, err := coordinator.Acquire(ctx, dialer.CallAdmissionInput{
		WorkspaceID:      "ws-1",
		SlotPollInterval: 2 * time.Millisecond,
		SlotPollTimeout:  500 * time.Millisecond,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Acquire() error = %v, want context.Canceled", err)
	}
	if lease != nil {
		t.Fatalf("Acquire() lease = %#v, want nil", lease)
	}
	if inflight.refreshCalls != 0 || len(inflight.released) != 0 {
		t.Fatalf("inflight must not be touched on ctx cancel")
	}
}

func TestCallAdmissionCoordinatorAcquireBalanceCheckError(t *testing.T) {
	slotGate := &testSlotGate{results: []bool{true}}
	coordinator := NewCallAdmissionCoordinator(
		&stubCachedBalanceChecker{err: errors.New("redis down")},
		&stubPricer{telephony: workspace_pricing.PriceResult{PriceMicros: 50_000}},
		&stubInflightReserver{reserveOK: true},
		slotGate,
		nil,
		nil,
	)

	lease, err := coordinator.Acquire(context.Background(), dialer.CallAdmissionInput{WorkspaceID: "ws-1"})
	if !errors.Is(err, dialer.ErrBalanceCheckFailed) {
		t.Fatalf("Acquire() error = %v, want ErrBalanceCheckFailed", err)
	}
	if lease != nil {
		t.Fatalf("Acquire() lease = %#v, want nil", lease)
	}
	if slotGate.releaseCalls != 1 {
		t.Fatalf("slot release calls = %d, want 1 (must release on inflight failure)", slotGate.releaseCalls)
	}
}

func TestCallAdmissionCoordinatorAcquireReserveError(t *testing.T) {
	slotGate := &testSlotGate{results: []bool{true}}
	inflight := &stubInflightReserver{reserveErr: errors.New("lua error")}
	coordinator := NewCallAdmissionCoordinator(
		&stubCachedBalanceChecker{balance: 100_000},
		&stubPricer{telephony: workspace_pricing.PriceResult{PriceMicros: 50_000}},
		inflight,
		slotGate,
		nil,
		nil,
	)

	lease, err := coordinator.Acquire(context.Background(), dialer.CallAdmissionInput{WorkspaceID: "ws-1"})
	if !errors.Is(err, dialer.ErrReservationFailed) {
		t.Fatalf("Acquire() error = %v, want ErrReservationFailed", err)
	}
	if lease != nil {
		t.Fatalf("Acquire() lease = %#v, want nil", lease)
	}
	if slotGate.releaseCalls != 1 {
		t.Fatalf("slot release calls = %d, want 1", slotGate.releaseCalls)
	}
}

func TestCallAdmissionCoordinatorAcquireZeroPrice(t *testing.T) {
	slotGate := &testSlotGate{results: []bool{true}}
	inflight := &stubInflightReserver{reserveOK: true}
	coordinator := NewCallAdmissionCoordinator(
		&stubCachedBalanceChecker{balance: 100_000},
		&stubPricer{telephony: workspace_pricing.PriceResult{PriceMicros: 0}},
		inflight,
		slotGate,
		nil,
		nil,
	)

	lease, err := coordinator.Acquire(context.Background(), dialer.CallAdmissionInput{WorkspaceID: "ws-1"})
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if !lease.SlotAcquired {
		t.Fatalf("expected slot acquired on zero-price call")
	}
	if lease.ReservedMicros != 0 {
		t.Fatalf("ReservedMicros = %d, want 0 (no inflight reservation at zero price)", lease.ReservedMicros)
	}
	if inflight.refreshCalls != 0 {
		t.Fatalf("RefreshTTL must not be called when no reservation was made")
	}

	if err := coordinator.Release(lease); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	if len(inflight.released) != 0 {
		t.Fatalf("inflight must not be released when nothing was reserved")
	}
	if slotGate.releaseCalls != 1 {
		t.Fatalf("slot release should still fire, got %d", slotGate.releaseCalls)
	}
}

func TestCallAdmissionCoordinatorRefreshNoop(t *testing.T) {
	inflight := &stubInflightReserver{}
	coordinator := NewCallAdmissionCoordinator(
		&stubCachedBalanceChecker{},
		&stubPricer{},
		inflight,
		nil,
		nil,
		nil,
	)

	cases := []*dialer.CallAdmissionLease{
		nil,
		{WorkspaceID: ""},
		{WorkspaceID: "ws-1", ReservedMicros: 0},
		{WorkspaceID: "ws-1", ReservedMicros: 1},
	}
	ttls := []time.Duration{time.Minute, time.Minute, time.Minute, 0}
	for i, lease := range cases {
		if err := coordinator.Refresh(lease, ttls[i]); err != nil {
			t.Fatalf("case %d: Refresh() error = %v, want nil", i, err)
		}
	}
	if inflight.refreshCalls != 0 {
		t.Fatalf("refresh-noop must not touch reserver; refreshCalls = %d", inflight.refreshCalls)
	}
}

func TestCallAdmissionCoordinatorRefreshError(t *testing.T) {
	inflight := &stubInflightReserver{refreshErr: errors.New("redis timeout")}
	coordinator := NewCallAdmissionCoordinator(
		&stubCachedBalanceChecker{},
		&stubPricer{},
		inflight,
		nil,
		nil,
		nil,
	)

	lease := &dialer.CallAdmissionLease{WorkspaceID: "ws-1", ReservedMicros: 1_000}
	err := coordinator.Refresh(lease, time.Minute)
	if !errors.Is(err, dialer.ErrReservationFailed) {
		t.Fatalf("Refresh() error = %v, want ErrReservationFailed", err)
	}
}

func TestCallAdmissionCoordinatorReleaseNilLease(t *testing.T) {
	inflight := &stubInflightReserver{}
	slotGate := &testSlotGate{}
	coordinator := NewCallAdmissionCoordinator(
		&stubCachedBalanceChecker{},
		&stubPricer{},
		inflight,
		slotGate,
		nil,
		nil,
	)

	if err := coordinator.Release(nil); err != nil {
		t.Fatalf("Release(nil) error = %v, want nil", err)
	}
	if len(inflight.released) != 0 || slotGate.releaseCalls != 0 {
		t.Fatalf("Release(nil) must not touch deps")
	}
}

func TestCallAdmissionCoordinatorReleaseInflightError(t *testing.T) {
	inflight := &stubInflightReserver{releaseErr: errors.New("lua error")}
	slotGate := &testSlotGate{}
	coordinator := NewCallAdmissionCoordinator(
		&stubCachedBalanceChecker{},
		&stubPricer{},
		inflight,
		slotGate,
		nil,
		nil,
	)

	lease := &dialer.CallAdmissionLease{
		WorkspaceID:    "ws-1",
		ReservedMicros: 50_000,
		SlotAcquired:   true,
	}
	err := coordinator.Release(lease)
	if !errors.Is(err, dialer.ErrReservationFailed) {
		t.Fatalf("Release() error = %v, want ErrReservationFailed", err)
	}
	if slotGate.releaseCalls != 1 {
		t.Fatalf("slot MUST be released even when inflight fails; releaseCalls = %d", slotGate.releaseCalls)
	}
	if lease.ReservedMicros != 0 || lease.SlotAcquired {
		t.Fatalf("lease must be cleared after Release even on inflight error: %#v", lease)
	}
}

func TestCallAdmissionCoordinatorReleaseNilSlotGate(t *testing.T) {
	inflight := &stubInflightReserver{}
	coordinator := NewCallAdmissionCoordinator(
		&stubCachedBalanceChecker{},
		&stubPricer{},
		inflight,
		nil,
		nil,
		nil,
	)

	lease := &dialer.CallAdmissionLease{
		WorkspaceID:    "ws-1",
		ReservedMicros: 25_000,
		SlotAcquired:   false,
	}
	if err := coordinator.Release(lease); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	if len(inflight.released) != 1 || inflight.released[0] != 25_000 {
		t.Fatalf("inflight release not recorded correctly: %#v", inflight.released)
	}
}

func TestCallAdmissionCoordinatorNilLoggerDefaults(t *testing.T) {
	coordinator := NewCallAdmissionCoordinator(
		&stubCachedBalanceChecker{balance: 100_000},
		&stubPricer{telephony: workspace_pricing.PriceResult{PriceMicros: 50_000}},
		&stubInflightReserver{reserveOK: true, refreshErr: errors.New("forces logger path")},
		&testSlotGate{results: []bool{true}},
		nil,
		nil,
	)

	lease, err := coordinator.Acquire(context.Background(), dialer.CallAdmissionInput{WorkspaceID: "ws-1"})
	if err != nil {
		t.Fatalf("Acquire() error = %v (RefreshTTL errors must not fail admission)", err)
	}
	if lease == nil || !lease.SlotAcquired {
		t.Fatalf("expected lease with acquired slot, got %#v", lease)
	}
}
