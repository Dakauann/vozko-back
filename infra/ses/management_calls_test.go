package ses

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	es "vozko/domain/emailsending"
)

type expiringState struct {
	mu      sync.Mutex
	values  map[string]time.Time
	failing atomic.Bool
}

func newExpiringState() *expiringState {
	return &expiringState{values: map[string]time.Time{}}
}

var errStoreDown = errors.New("redis: connection refused")

func (s *expiringState) live(key string, now time.Time) bool {
	until, ok := s.values[key]
	return ok && now.Before(until)
}

func (s *expiringState) SetNX(key, _ string, ttl time.Duration) (bool, error) {
	if s.failing.Load() {
		return false, errStoreDown
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if s.live(key, now) {
		return false, nil
	}
	s.values[key] = now.Add(ttl)
	return true, nil
}

func (s *expiringState) SetString(key, _ string, ttl time.Duration) error {
	if s.failing.Load() {
		return errStoreDown
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[key] = time.Now().Add(ttl)
	return nil
}

func (s *expiringState) Exists(key string) (bool, error) {
	if s.failing.Load() {
		return false, errStoreDown
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.live(key, time.Now()), nil
}

type recordingOps struct {
	mu     sync.Mutex
	alerts []string
}

func (o *recordingOps) Alert(_ context.Context, subject, detail string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.alerts = append(o.alerts, subject+": "+detail)
	return nil
}

func (o *recordingOps) count() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return len(o.alerts)
}

const testSpacing = 40 * time.Millisecond

func testLimiter(t *testing.T, state LimiterState, ops OpsAlerter) *ManagementCalls {
	t.Helper()
	calls, err := NewManagementCalls(state, ops, ManagementCallsConfig{Spacing: testSpacing, Poll: 2 * time.Millisecond, WantedTTL: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	return calls
}

type grantLog struct {
	mu    sync.Mutex
	at    []time.Time
	order []string
}

func (g *grantLog) add(label string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.at = append(g.at, time.Now())
	g.order = append(g.order, label)
}

func (g *grantLog) minimumGap() time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()
	times := slices.Clone(g.at)
	slices.SortFunc(times, func(a, b time.Time) int { return a.Compare(b) })
	gap := time.Hour
	for i := 1; i < len(times); i++ {
		gap = min(gap, times[i].Sub(times[i-1]))
	}
	return gap
}

func TestReplicasSharingTheStoreCallAtMostOncePerSpacing(t *testing.T) {
	state := newExpiringState()
	replicas := []*ManagementCalls{testLimiter(t, state, &recordingOps{}), testLimiter(t, state, &recordingOps{})}
	log := &grantLog{}
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lane := es.ProviderLanes[i%len(es.ProviderLanes)]
			if err := replicas[i%2].Await(context.Background(), lane); err != nil {
				t.Error(err)
				return
			}
			log.add(lane.String())
		}()
	}
	wg.Wait()
	if len(log.at) != 8 {
		t.Fatalf("grants = %d, want 8", len(log.at))
	}
	if gap := log.minimumGap(); gap < testSpacing-5*time.Millisecond {
		t.Fatalf("two calls were granted %s apart, want at least %s", gap, testSpacing)
	}
}

func TestAHigherLaneOvertakesLowerLanesQueuedBeforeIt(t *testing.T) {
	cases := []struct {
		name     string
		replicas int
	}{
		{name: "same replica", replicas: 1},
		{name: "across replicas", replicas: 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			state := newExpiringState()
			limiters := []*ManagementCalls{testLimiter(t, state, &recordingOps{})}
			if tc.replicas == 2 {
				limiters = append(limiters, testLimiter(t, state, &recordingOps{}))
			}
			if _, err := state.SetNX(managementSlotKey, "held", 3*testSpacing); err != nil {
				t.Fatal(err)
			}
			log := &grantLog{}
			var wg sync.WaitGroup
			start := func(limiter *ManagementCalls, lane es.ProviderLane, label string) {
				wg.Add(1)
				go func() {
					defer wg.Done()
					if err := limiter.Await(context.Background(), lane); err != nil {
						t.Error(err)
						return
					}
					log.add(label)
				}()
			}
			start(limiters[0], es.LaneBackground, "background-1")
			start(limiters[0], es.LaneBackground, "background-2")
			time.Sleep(10 * time.Millisecond)
			start(limiters[len(limiters)-1], es.LaneProtect, "protect")
			wg.Wait()
			if log.order[0] != "protect" {
				t.Fatalf("grant order = %v, want the protect lane first", log.order)
			}
		})
	}
}

func TestAnUnreachableStorePacesEachReplicaAtTheFullIntervalAndAlerts(t *testing.T) {
	state := newExpiringState()
	state.failing.Store(true)
	ops := &recordingOps{}
	limiter := testLimiter(t, state, ops)
	log := &grantLog{}
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := limiter.Await(context.Background(), es.LaneProtect); err != nil {
				t.Error(err)
				return
			}
			log.add("call")
		}()
	}
	wg.Wait()
	if gap := log.minimumGap(); gap < testSpacing-5*time.Millisecond {
		t.Fatalf("without the store two calls were %s apart, want at least %s", gap, testSpacing)
	}
	if ops.count() != 1 || !strings.Contains(ops.alerts[0], "Redis") {
		t.Fatalf("alerts = %v, want exactly one throttled alert", ops.alerts)
	}
}

func TestAWaitingCallerThatGivesUpIsNeverGranted(t *testing.T) {
	state := newExpiringState()
	limiter := testLimiter(t, state, &recordingOps{})
	if _, err := state.SetNX(managementSlotKey, "held", time.Hour); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := limiter.Await(ctx, es.LaneBackground); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the deadline", err)
	}
	if limiter.waiting(es.LaneBackground) {
		t.Fatal("a caller that gave up must leave the queue")
	}
}

func TestNewManagementCallsRefusesAMissingStore(t *testing.T) {
	if _, err := NewManagementCalls(nil, &recordingOps{}, ManagementCallsConfig{}); !errors.Is(err, ErrIncompleteConfig) {
		t.Fatalf("err = %v", err)
	}
	if _, err := NewManagementCalls(newExpiringState(), nil, ManagementCallsConfig{}); !errors.Is(err, ErrIncompleteConfig) {
		t.Fatalf("err = %v", err)
	}
}

func TestTheDefaultSpacingIsTheSESManagementRatePlusAnArrivalMargin(t *testing.T) {
	cfg := DefaultManagementCallsConfig()
	if cfg.Spacing <= es.SESManagementCallInterval || cfg.Spacing > es.SESManagementCallInterval+200*time.Millisecond {
		t.Fatalf("spacing = %s", cfg.Spacing)
	}
}

func TestALimiterWithItsOwnKeysNeverSharesTheSESSlot(t *testing.T) {
	state := newExpiringState()
	if _, err := state.SetNX(managementSlotKey, "held", time.Hour); err != nil {
		t.Fatal(err)
	}
	other, err := NewManagementCalls(state, &recordingOps{}, ManagementCallsConfig{
		Spacing: testSpacing, Poll: 2 * time.Millisecond, WantedTTL: 20 * time.Millisecond,
		Service: "Route 53", SlotKey: "route53:calls:slot", WantedPrefix: "route53:calls:wanted:",
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := other.Await(ctx, es.LaneAccount); err != nil {
		t.Fatalf("a held SES slot blocked another service's limiter: %v", err)
	}
	if live, _ := state.Exists("route53:calls:slot"); !live {
		t.Fatal("the limiter did not take its own slot key")
	}
	if live, _ := state.Exists("route53:calls:wanted:" + es.LaneAccount.String()); !live {
		t.Fatal("the limiter did not announce its lane under its own prefix")
	}
}

func TestAnUnreachableStoreAlertNamesTheServiceItPaces(t *testing.T) {
	state := newExpiringState()
	state.failing.Store(true)
	ops := &recordingOps{}
	limiter, err := NewManagementCalls(state, ops, ManagementCallsConfig{
		Spacing: testSpacing, Poll: 2 * time.Millisecond, WantedTTL: 20 * time.Millisecond,
		Service: "Route 53", SlotKey: "route53:calls:slot", WantedPrefix: "route53:calls:wanted:",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := limiter.Await(context.Background(), es.LaneAccount); err != nil {
		t.Fatal(err)
	}
	if ops.count() != 1 || !strings.Contains(ops.alerts[0], "Route 53") || strings.Contains(ops.alerts[0], "SES") {
		t.Fatalf("alerts = %v, want one naming Route 53", ops.alerts)
	}
}

func TestTheDefaultLimiterKeepsTheSESKeysAndName(t *testing.T) {
	cfg := DefaultManagementCallsConfig()
	if cfg.SlotKey != managementSlotKey || cfg.WantedPrefix != managementWantedPrefix || cfg.Service != "SES" {
		t.Fatalf("defaults = %+v", cfg)
	}
}
