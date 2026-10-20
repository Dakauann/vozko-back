package inbox_assignment_usecase

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ia "vozko/domain/inbox_assignment"
	wsc "vozko/domain/workspace_config"
)

type liveWorkload struct {
	repo  *statefulRepo
	base  map[string]int
	err   error
	calls int
}

func (w *liveWorkload) OpenConversations(_ string, userIDs []string) (map[string]int, error) {
	w.calls++
	if w.err != nil {
		return nil, w.err
	}
	out := make(map[string]int, len(userIDs))
	for _, id := range userIDs {
		out[id] = w.base[id]
	}
	if w.repo != nil {
		for _, a := range w.repo.assignments {
			if _, asked := out[a.AssignedUserID]; asked {
				out[a.AssignedUserID]++
			}
		}
	}
	return out, nil
}

type stubHandouts struct {
	last map[string]time.Time
	err  error
}

func (h *stubHandouts) LastHandouts(string, []string) (map[string]time.Time, error) {
	return h.last, h.err
}

func strategyConfig(strategy ia.Strategy, recipients ...ia.Recipient) *wsc.WorkspaceConfig {
	return &wsc.WorkspaceConfig{RouletteStrategy: strategy, RouletteRecipients: recipients}
}

func strategyService(repo *statefulRepo, online *mockEligible, cfg *wsc.WorkspaceConfig) *AssignmentService {
	svc := NewAssignmentService(repo, online, defaultResolver("ws-1", ""), &rouletteConfig{cfg: cfg})
	svc.Candidates().SetClock(func() time.Time { return testNow })
	return svc
}

func assignMany(svc *AssignmentService, n int) []string {
	got := make([]string, 0, n)
	for i := 0; i < n; i++ {
		got = append(got, svc.EnsureAssignment(fmt.Sprintf("entry-%d", i), "whatsapp", "phone-1"))
	}
	return got
}

func tier(userID string, t int) ia.Recipient { return ia.Recipient{UserID: userID, Tier: t, Weight: 1} }

func weight(userID string, w int) ia.Recipient {
	return ia.Recipient{UserID: userID, Tier: 1, Weight: w}
}

func TestPriority_RotatesInsideTheBestOnlineTierAndNeverReachesUnlistedMembers(t *testing.T) {
	repo := newStatefulRepo()
	svc := strategyService(repo, defaultEligible("ana", "bob", "cid", "outsider"),
		strategyConfig(ia.StrategyPriority, tier("bob", 1), tier("ana", 1), tier("cid", 2)))

	assert.Equal(t, []string{"ana", "bob", "ana", "bob"}, assignMany(svc, 4))
}

func TestPriority_OverflowsToTheNextTierWhenTheTopTierIsOffline(t *testing.T) {
	repo := newStatefulRepo()
	svc := strategyService(repo, defaultEligible("cid", "dan"),
		strategyConfig(ia.StrategyPriority, tier("ana", 1), tier("cid", 2), tier("dan", 3)))

	assert.Equal(t, []string{"cid", "cid"}, assignMany(svc, 2))
}

func TestPriority_NobodyListedOnlineLeavesItUnassignedInsteadOfFallingBackToEveryone(t *testing.T) {
	repo := newStatefulRepo()
	svc := strategyService(repo, defaultEligible("outsider"),
		strategyConfig(ia.StrategyPriority, tier("ana", 1)))

	assert.Empty(t, svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
	assert.Empty(t, repo.assignments)
}

func TestPriority_EmptyListAssignsNobody(t *testing.T) {
	repo := newStatefulRepo()
	svc := strategyService(repo, defaultEligible("ana"), strategyConfig(ia.StrategyPriority))

	assert.Empty(t, svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
}

func TestPriority_ListedMemberOutsideTheDepartmentNeverReceives(t *testing.T) {
	repo := newStatefulRepo()
	online := deptEligible([]string{"ana", "bob"}, "ws-1:dept-1", []string{"bob"})
	svc := NewAssignmentService(repo, online, defaultResolver("ws-1", "dept-1"),
		&rouletteConfig{cfg: strategyConfig(ia.StrategyPriority, tier("ana", 1), tier("bob", 2))})

	assert.Equal(t, "bob", svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
}

func TestPriority_OverLastSeenUsesTheWindow(t *testing.T) {
	repo := newStatefulRepo()
	cfg := strategyConfig(ia.StrategyPriority, tier("ana", 1), tier("bob", 2))
	cfg.RouletteMode = wsc.RouletteModeLastSeen
	cfg.RouletteLastSeenWindowHours = 48
	svc := lastSeenService(repo, defaultEligible(),
		&stubRoster{members: []string{"ana", "bob"}},
		&stubLastSeen{seen: map[string]time.Time{"ana": hoursAgo(72), "bob": hoursAgo(2)}},
		&rouletteConfig{cfg: cfg}, "")

	assert.Equal(t, "bob", svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
}

func TestWeighted_SharesInProportionAndStoresTheSlotKey(t *testing.T) {
	repo := newStatefulRepo()
	svc := strategyService(repo, defaultEligible("ana", "bob"),
		strategyConfig(ia.StrategyWeighted, weight("ana", 2), weight("bob", 1)))

	got := assignMany(svc, 6)
	assert.Equal(t, []string{"ana", "bob", "ana", "ana", "bob", "ana"}, got)
	assert.Equal(t, "ana#2", repo.rrStates[rrKey("ws-1", "phone-1", "")].LastAssignedUserID)
}

func TestWeighted_OfflineMemberDropsOutOfTheSequence(t *testing.T) {
	repo := newStatefulRepo()
	svc := strategyService(repo, defaultEligible("bob"),
		strategyConfig(ia.StrategyWeighted, weight("ana", 5), weight("bob", 1)))

	assert.Equal(t, []string{"bob", "bob"}, assignMany(svc, 2))
}

func TestLeastBusy_PicksTheFewestOpenAndBalancesAsWorkArrives(t *testing.T) {
	repo := newStatefulRepo()
	svc := strategyService(repo, defaultEligible("ana", "bob", "cid"), strategyConfig(ia.StrategyLeastBusy))
	svc.SetWorkload(&liveWorkload{repo: repo, base: map[string]int{"ana": 2, "bob": 0, "cid": 1}})
	svc.SetHandouts(&stubHandouts{})

	assert.Equal(t, []string{"bob", "bob", "cid", "ana"}, assignMany(svc, 4))
}

func TestLeastBusy_FailsClosedWhenTheWorkloadCannotBeRead(t *testing.T) {
	repo := newStatefulRepo()
	svc := strategyService(repo, defaultEligible("ana"), strategyConfig(ia.StrategyLeastBusy))
	svc.SetWorkload(&liveWorkload{err: errors.New("db down")})
	svc.SetHandouts(&stubHandouts{})

	assert.Empty(t, svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
}

func TestLeastBusy_FailsClosedWhenTheReadersAreNotWired(t *testing.T) {
	repo := newStatefulRepo()
	svc := strategyService(repo, defaultEligible("ana"), strategyConfig(ia.StrategyLeastBusy))

	assert.Empty(t, svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
}

func TestCapacity_SkipsFullMembersUnderEveryStrategy(t *testing.T) {
	repo := newStatefulRepo()
	cfg := strategyConfig(ia.StrategyRoundRobin)
	cfg.RouletteMaxOpenPerMember = 2
	svc := strategyService(repo, defaultEligible("ana", "bob"), cfg)
	workload := &liveWorkload{repo: repo, base: map[string]int{"ana": 2}}
	svc.SetWorkload(workload)

	assert.Equal(t, []string{"bob", "bob", ""}, assignMany(svc, 3))
	assert.Equal(t, 3, workload.calls)
}

func TestCapacity_UnreadableWorkloadAssignsNobody(t *testing.T) {
	repo := newStatefulRepo()
	cfg := strategyConfig(ia.StrategyRoundRobin)
	cfg.RouletteMaxOpenPerMember = 5
	svc := strategyService(repo, defaultEligible("ana"), cfg)
	svc.SetWorkload(&liveWorkload{err: errors.New("db down")})

	assert.Empty(t, svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
}

func TestRoundRobinWithoutCapacityNeverReadsTheWorkload(t *testing.T) {
	repo := newStatefulRepo()
	svc := strategyService(repo, defaultEligible("ana", "bob"), strategyConfig(ia.StrategyRoundRobin))
	workload := &liveWorkload{repo: repo}
	svc.SetWorkload(workload)

	assert.Equal(t, []string{"ana", "bob"}, assignMany(svc, 2))
	assert.Zero(t, workload.calls)
}

func TestLongestIdle_PicksWhoWaitedLongestSinceTheirLastConversation(t *testing.T) {
	repo := newStatefulRepo()
	svc := strategyService(repo, defaultEligible("ana", "bob", "cid"), strategyConfig(ia.StrategyLongestIdle))
	svc.SetHandouts(&stubHandouts{last: map[string]time.Time{"ana": hoursAgo(1), "bob": hoursAgo(3), "cid": hoursAgo(2)}})

	assert.Equal(t, "bob", svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
}

func TestLongestIdle_FailsClosedWhenHandoutsCannotBeRead(t *testing.T) {
	repo := newStatefulRepo()
	svc := strategyService(repo, defaultEligible("ana"), strategyConfig(ia.StrategyLongestIdle))
	svc.SetHandouts(&stubHandouts{err: errors.New("db down")})

	assert.Empty(t, svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
}

func TestRandom_PicksTheDrawnMember(t *testing.T) {
	repo := newStatefulRepo()
	svc := strategyService(repo, defaultEligible("ana", "bob", "cid"), strategyConfig(ia.StrategyRandom))
	svc.Candidates().SetDraw(func(n int) int { return n - 1 })

	assert.Equal(t, "cid", svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
}

func TestClaim_LeavesTheConversationForTheFirstWhoOpensIt(t *testing.T) {
	repo := newStatefulRepo()
	svc := strategyService(repo, defaultEligible("ana", "bob"), strategyConfig(ia.StrategyClaim))

	assert.Empty(t, svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
	claimed, err := svc.AssignOnOpen("entry-1", "whatsapp", "phone-1", "ws-1", "bob")
	require.NoError(t, err)
	assert.True(t, claimed)
}

func TestResolve_ReportsWhyThePoolIsEmpty(t *testing.T) {
	cases := []struct {
		name   string
		cfg    *wsc.WorkspaceConfig
		online []string
		want   string
	}{
		{"nobody listed is available", strategyConfig(ia.StrategyPriority, tier("ana", 1)), []string{"bob"}, PoolReasonNoRecipientAvailable},
		{"claim picks nobody", strategyConfig(ia.StrategyClaim), []string{"bob"}, PoolReasonOnline},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pool := NewCandidateResolver(defaultEligible(tc.online...)).Resolve("ws-1", "", false, tc.cfg)
			assert.False(t, pool.CanPick())
			assert.Equal(t, tc.want, pool.Reason)
			assert.Equal(t, tc.cfg.EffectiveRouletteStrategy(), pool.Strategy)
		})
	}
}

func TestRescue_PriorityEscalatesToTheNextTierWhenTheTopTierIgnoresIt(t *testing.T) {
	f := newRescueFixture(t,
		[]string{"ana", "bob", "cid"},
		map[string]time.Time{"ana": hoursAgo(1), "bob": hoursAgo(2), "cid": hoursAgo(3)},
		openInterval("entry-1", "ana", minutesAgo(20)))
	f.cfg.cfg = rescueConfigWith(func(c *wsc.WorkspaceConfig) {
		c.RouletteStrategy = ia.StrategyPriority
		c.RouletteRecipients = []ia.Recipient{tier("ana", 1), tier("cid", 2)}
	})
	f.seedAssignment("entry-1", "ana")

	require.NoError(t, f.job.Execute(context.Background()))
	assert.Equal(t, "cid", f.ownerOf("entry-1"))
}

func TestRescue_ClaimReleasesTheConversationToTheTeam(t *testing.T) {
	f := newRescueFixture(t,
		[]string{"ana", "bob"},
		map[string]time.Time{"ana": hoursAgo(1), "bob": hoursAgo(2)},
		openInterval("entry-1", "ana", minutesAgo(20)))
	f.cfg.cfg = rescueConfigWith(func(c *wsc.WorkspaceConfig) { c.RouletteStrategy = ia.StrategyClaim })
	f.seedAssignment("entry-1", "ana")

	require.NoError(t, f.job.Execute(context.Background()))
	assert.Empty(t, f.ownerOf("entry-1"))
}
