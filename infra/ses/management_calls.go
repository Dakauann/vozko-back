package ses

import (
	"context"
	"fmt"
	"log"
	"slices"
	"sync"
	"time"

	"github.com/aws/smithy-go/middleware"

	"vozko/domain/cache"
	es "vozko/domain/emailsending"
)

const (
	managementSlotKey      = "ses:management:slot"
	managementWantedPrefix = "ses:management:wanted:"
	managementService      = "SES"
	arrivalMargin          = 100 * time.Millisecond
	managementPoll         = 50 * time.Millisecond
	managementWantedTTL    = 500 * time.Millisecond
	storeAlertEvery        = time.Minute
	retryMiddlewareID      = "Retry"
)

type LimiterState interface {
	SetNX(key, value string, ttl time.Duration) (bool, error)
	SetString(key, value string, ttl time.Duration) error
	Exists(key string) (bool, error)
}

type OpsAlerter interface {
	Alert(ctx context.Context, subject, detail string) error
}

type ManagementCallsConfig struct {
	Spacing      time.Duration
	Poll         time.Duration
	WantedTTL    time.Duration
	Service      string
	SlotKey      string
	WantedPrefix string
}

func DefaultManagementCallsConfig() ManagementCallsConfig {
	return ManagementCallsConfig{
		Spacing:      es.SESManagementCallInterval + arrivalMargin,
		Poll:         managementPoll,
		WantedTTL:    managementWantedTTL,
		Service:      managementService,
		SlotKey:      managementSlotKey,
		WantedPrefix: managementWantedPrefix,
	}
}

type ManagementCalls struct {
	state  LimiterState
	ops    OpsAlerter
	cfg    ManagementCallsConfig
	local  *cache.LocalSpacing
	alerts *cache.LocalSpacing

	mu     sync.Mutex
	queues map[es.ProviderLane][]uint64
	seq    uint64
}

var _ es.ProviderCallLimiter = (*ManagementCalls)(nil)

func NewManagementCalls(state LimiterState, ops OpsAlerter, cfg ManagementCallsConfig) (*ManagementCalls, error) {
	if state == nil || ops == nil {
		return nil, fmt.Errorf("%w: management call limiter store and ops alerts", ErrIncompleteConfig)
	}
	defaults := DefaultManagementCallsConfig()
	if cfg.Spacing <= 0 {
		cfg.Spacing = defaults.Spacing
	}
	if cfg.Poll <= 0 {
		cfg.Poll = defaults.Poll
	}
	if cfg.WantedTTL <= 0 {
		cfg.WantedTTL = defaults.WantedTTL
	}
	if cfg.Service == "" {
		cfg.Service = defaults.Service
	}
	if cfg.SlotKey == "" {
		cfg.SlotKey = defaults.SlotKey
	}
	if cfg.WantedPrefix == "" {
		cfg.WantedPrefix = defaults.WantedPrefix
	}
	return &ManagementCalls{
		state:  state,
		ops:    ops,
		cfg:    cfg,
		local:  cache.NewLocalSpacing(cfg.Spacing),
		alerts: cache.NewLocalSpacing(storeAlertEvery),
		queues: map[es.ProviderLane][]uint64{},
	}, nil
}

func (m *ManagementCalls) Await(ctx context.Context, lane es.ProviderLane) error {
	if !lane.Valid() {
		lane = es.LaneAccount
	}
	ticket := m.join(lane)
	defer m.leave(lane, ticket)
	for {
		if m.atFront(lane, ticket) && m.claim(ctx, lane) {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("ses: waiting for a management call slot: %w", ctx.Err())
		case <-time.After(m.cfg.Poll):
		}
	}
}

func (m *ManagementCalls) join(lane es.ProviderLane) uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	m.queues[lane] = append(m.queues[lane], m.seq)
	return m.seq
}

func (m *ManagementCalls) leave(lane es.ProviderLane, ticket uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queues[lane] = slices.DeleteFunc(m.queues[lane], func(t uint64) bool { return t == ticket })
}

func (m *ManagementCalls) waiting(lane es.ProviderLane) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.queues[lane]) > 0
}

func (m *ManagementCalls) atFront(lane es.ProviderLane, ticket uint64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	queue := m.queues[lane]
	if len(queue) == 0 || queue[0] != ticket {
		return false
	}
	return es.LaneMayCall(lane, func(other es.ProviderLane) bool { return len(m.queues[other]) > 0 })
}

func (m *ManagementCalls) claim(ctx context.Context, lane es.ProviderLane) bool {
	granted, err := m.claimShared(lane)
	if err != nil {
		m.alertStore(ctx, err)
		return m.local.Take(time.Now())
	}
	if granted {
		m.local.Mark(time.Now())
	}
	return granted
}

func (m *ManagementCalls) claimShared(lane es.ProviderLane) (bool, error) {
	if err := m.state.SetString(m.wantedKey(lane), "1", m.cfg.WantedTTL); err != nil {
		return false, err
	}
	var storeErr error
	mayCall := es.LaneMayCall(lane, func(other es.ProviderLane) bool {
		if storeErr != nil {
			return true
		}
		waiting, err := m.state.Exists(m.wantedKey(other))
		if err != nil {
			storeErr = err
			return true
		}
		return waiting
	})
	if storeErr != nil {
		return false, storeErr
	}
	if !mayCall {
		return false, nil
	}
	return m.state.SetNX(m.cfg.SlotKey, "1", m.cfg.Spacing)
}

func (m *ManagementCalls) alertStore(ctx context.Context, err error) {
	if !m.alerts.Take(time.Now()) {
		return
	}
	detail := fmt.Sprintf("o limite de chamadas do %s passa a valer por réplica (uma a cada %s) até o Redis voltar: %v", m.cfg.Service, m.cfg.Spacing, err)
	log.Printf("[ses] %s call limiter store unavailable: %s", m.cfg.Service, detail)
	if alertErr := m.ops.Alert(ctx, "Limitador de chamadas do "+m.cfg.Service+" sem Redis", detail); alertErr != nil {
		log.Printf("[ses] ops alert failed: %v", alertErr)
	}
}

func (m *ManagementCalls) wantedKey(lane es.ProviderLane) string {
	return m.cfg.WantedPrefix + lane.String()
}

type callGate struct {
	limiter es.ProviderCallLimiter
	selects func(operation string) bool
}

func (callGate) ID() string { return "ProviderCallRate" }

func (g callGate) HandleFinalize(ctx context.Context, in middleware.FinalizeInput, next middleware.FinalizeHandler) (middleware.FinalizeOutput, middleware.Metadata, error) {
	if g.selects(middleware.GetOperationName(ctx)) {
		if err := g.limiter.Await(ctx, es.ProviderLaneOf(ctx)); err != nil {
			return middleware.FinalizeOutput{}, middleware.Metadata{}, err
		}
	}
	return next.HandleFinalize(ctx, in)
}

func PaceManagementCalls(limiter es.ProviderCallLimiter) func(*middleware.Stack) error {
	return PaceCalls(limiter, es.SESManagementCall)
}

func PaceCalls(limiter es.ProviderCallLimiter, selects func(operation string) bool) func(*middleware.Stack) error {
	return func(stack *middleware.Stack) error {
		return stack.Finalize.Insert(callGate{limiter: limiter, selects: selects}, retryMiddlewareID, middleware.After)
	}
}
