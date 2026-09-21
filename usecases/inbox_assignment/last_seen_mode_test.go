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

var testNow = time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)

func hoursAgo(h float64) time.Time {
	return testNow.Add(-time.Duration(h * float64(time.Hour)))
}

type rouletteConfig struct {
	cfg *wsc.WorkspaceConfig
	err error
}

func (c *rouletteConfig) GetByWorkspaceID(context.Context, string) (*wsc.WorkspaceConfig, error) {
	if c.err != nil {
		return nil, c.err
	}
	return c.cfg, nil
}

func lastSeenConfig(windowHours int) *rouletteConfig {
	return &rouletteConfig{cfg: &wsc.WorkspaceConfig{
		RouletteMode:                wsc.RouletteModeLastSeen,
		RouletteLastSeenWindowHours: windowHours,
	}}
}

type stubRoster struct {
	members []string
	err     error
	calls   []string
}

func (r *stubRoster) ListRouletteMembers(workspaceID, departmentID string, skipAdmins bool) ([]string, error) {
	r.calls = append(r.calls, fmt.Sprintf("%s|%s|%v", workspaceID, departmentID, skipAdmins))
	if r.err != nil {
		return nil, r.err
	}
	return append([]string(nil), r.members...), nil
}

type stubLastSeen struct {
	seen  map[string]time.Time
	err   error
	calls int
}

func (s *stubLastSeen) LastSeen(string, []string) (map[string]time.Time, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return s.seen, nil
}

func lastSeenService(
	repo ia.Repository,
	online *mockEligible,
	roster *stubRoster,
	seen *stubLastSeen,
	cfg WorkspaceConfigProvider,
	deptID string,
) *AssignmentService {
	svc := NewAssignmentService(repo, online, defaultResolver("ws-1", deptID), cfg)
	if roster != nil {
		svc.SetRoster(roster)
	}
	if seen != nil {
		svc.SetPresence(seen)
	}
	svc.Candidates().SetClock(func() time.Time { return testNow })
	return svc
}

func TestLastSeen_AssignsToOfflineMembersByRecency(t *testing.T) {
	repo := newStatefulRepo()
	svc := lastSeenService(repo,
		defaultEligible(),
		&stubRoster{members: []string{"dan", "cid", "bob", "ana"}},
		&stubLastSeen{seen: map[string]time.Time{
			"ana": hoursAgo(1),
			"bob": hoursAgo(5),
			"cid": hoursAgo(20),
			"dan": hoursAgo(47),
		}},
		lastSeenConfig(48), "")

	var got []string
	for i := 0; i < 5; i++ {
		got = append(got, svc.EnsureAssignment(fmt.Sprintf("entry-%d", i), "whatsapp", "phone-1"))
	}
	assert.Equal(t, []string{"ana", "bob", "cid", "dan", "ana"}, got)
}

func TestLastSeen_StaleMembersAreExcluded(t *testing.T) {
	repo := newStatefulRepo()
	svc := lastSeenService(repo,
		defaultEligible(),
		&stubRoster{members: []string{"ana", "stale"}},
		&stubLastSeen{seen: map[string]time.Time{
			"ana":   hoursAgo(10),
			"stale": hoursAgo(72),
		}},
		lastSeenConfig(48), "")

	for i := 0; i < 4; i++ {
		assert.Equal(t, "ana", svc.EnsureAssignment(fmt.Sprintf("entry-%d", i), "whatsapp", "phone-1"))
	}
}

func TestLastSeen_EverybodyStaleAndNobodyOnlineLeavesItUnassigned(t *testing.T) {
	repo := newStatefulRepo()
	svc := lastSeenService(repo,
		defaultEligible(),
		&stubRoster{members: []string{"ana", "bob"}},
		&stubLastSeen{seen: map[string]time.Time{
			"ana": hoursAgo(72),
			"bob": hoursAgo(96),
		}},
		lastSeenConfig(48), "")

	assert.Equal(t, "", svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
	assert.Nil(t, repo.assignments[assignmentKey("ws-1", "entry-1", "whatsapp")])
}

func TestLastSeen_ConnectedMemberSortsToTheHead(t *testing.T) {
	repo := newStatefulRepo()
	svc := lastSeenService(repo,
		defaultEligible("stale"),
		&stubRoster{members: []string{"ana", "stale"}},
		&stubLastSeen{seen: map[string]time.Time{
			"ana":   hoursAgo(1),
			"stale": hoursAgo(200),
		}},
		lastSeenConfig(48), "")

	assert.Equal(t, "stale", svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
	assert.Equal(t, "ana", svc.EnsureAssignment("entry-2", "whatsapp", "phone-1"))
}

func TestLastSeen_WindowBoundary(t *testing.T) {
	cases := []struct {
		name string
		age  float64
		want string
	}{
		{"just inside", 47.9, "edge"},
		{"just outside", 48.1, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := lastSeenService(newStatefulRepo(),
				defaultEligible(),
				&stubRoster{members: []string{"edge"}},
				&stubLastSeen{seen: map[string]time.Time{"edge": hoursAgo(tc.age)}},
				lastSeenConfig(48), "")
			assert.Equal(t, tc.want, svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
		})
	}
}

func TestLastSeen_WindowChangeTakesEffectImmediately(t *testing.T) {
	cfg := lastSeenConfig(48)
	repo := newStatefulRepo()
	svc := lastSeenService(repo,
		defaultEligible(),
		&stubRoster{members: []string{"ana"}},
		&stubLastSeen{seen: map[string]time.Time{"ana": hoursAgo(24)}},
		cfg, "")

	assert.Equal(t, "ana", svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))

	cfg.cfg.RouletteLastSeenWindowHours = 1
	assert.Equal(t, "", svc.EnsureAssignment("entry-2", "whatsapp", "phone-1"))
}

func TestLastSeen_NoPresenceDataFallsBackToTheOnlinePool(t *testing.T) {
	repo := newStatefulRepo()
	svc := lastSeenService(repo,
		defaultEligible("connected"),
		&stubRoster{members: []string{"ana", "bob", "connected"}},
		&stubLastSeen{seen: map[string]time.Time{}},
		lastSeenConfig(48), "")

	assert.Equal(t, "connected", svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))

	offline := lastSeenService(newStatefulRepo(),
		defaultEligible("connected"),
		&stubRoster{members: []string{"ana", "bob"}},
		&stubLastSeen{seen: map[string]time.Time{}},
		lastSeenConfig(48), "")
	assert.Equal(t, "connected", offline.EnsureAssignment("entry-1", "whatsapp", "phone-1"),
		"an empty last-seen ring must fall back to the connected pool")
}

func TestLastSeen_ReadErrorsFallBackToTheOnlinePool(t *testing.T) {
	boom := errors.New("db down")

	rosterErr := lastSeenService(newStatefulRepo(),
		defaultEligible("connected"),
		&stubRoster{err: boom},
		&stubLastSeen{},
		lastSeenConfig(48), "")
	assert.Equal(t, "connected", rosterErr.EnsureAssignment("entry-1", "whatsapp", "phone-1"))

	presenceErr := lastSeenService(newStatefulRepo(),
		defaultEligible("connected"),
		&stubRoster{members: []string{"ana"}},
		&stubLastSeen{err: boom},
		lastSeenConfig(48), "")
	assert.Equal(t, "connected", presenceErr.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
}

func TestLastSeen_UnwiredReadersFallBackToTheOnlinePool(t *testing.T) {
	svc := lastSeenService(newStatefulRepo(),
		defaultEligible("connected"),
		nil, nil,
		lastSeenConfig(48), "")
	assert.Equal(t, "connected", svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
}

func TestLastSeen_ConfigErrorUsesTheOnlinePool(t *testing.T) {
	svc := lastSeenService(newStatefulRepo(),
		defaultEligible("connected"),
		&stubRoster{members: []string{"ana"}},
		&stubLastSeen{seen: map[string]time.Time{"ana": hoursAgo(1)}},
		&rouletteConfig{err: errors.New("boom")}, "")
	assert.Equal(t, "connected", svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
}

func TestLastSeen_UnknownModeUsesTheOnlinePool(t *testing.T) {
	svc := lastSeenService(newStatefulRepo(),
		defaultEligible("connected"),
		&stubRoster{members: []string{"ana"}},
		&stubLastSeen{seen: map[string]time.Time{"ana": hoursAgo(1)}},
		&rouletteConfig{cfg: &wsc.WorkspaceConfig{RouletteMode: "garbage"}}, "")
	assert.Equal(t, "connected", svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
}

func TestLastSeen_PassesDepartmentAndSkipAdminsToTheRoster(t *testing.T) {
	roster := &stubRoster{members: []string{"ana"}}
	cfg := lastSeenConfig(48)
	cfg.cfg.SkipAdminAssignment = true

	svc := lastSeenService(newStatefulRepo(),
		defaultEligible(),
		roster,
		&stubLastSeen{seen: map[string]time.Time{"ana": hoursAgo(1)}},
		cfg, "dept-a")

	assert.Equal(t, "ana", svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
	require.Len(t, roster.calls, 1)
	assert.Equal(t, "ws-1|dept-a|true", roster.calls[0])
}

func TestLastSeen_PresenceForNonMembersIsIgnored(t *testing.T) {
	svc := lastSeenService(newStatefulRepo(),
		defaultEligible(),
		&stubRoster{members: []string{"ana"}},
		&stubLastSeen{seen: map[string]time.Time{
			"ana":    hoursAgo(10),
			"leaver": hoursAgo(1),
		}},
		lastSeenConfig(48), "")

	assert.Equal(t, "ana", svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
}

func TestLastSeen_FutureTimestampIsClamped(t *testing.T) {
	repo := newStatefulRepo()
	svc := lastSeenService(repo,
		defaultEligible(),
		&stubRoster{members: []string{"ana", "skewed"}},
		&stubLastSeen{seen: map[string]time.Time{
			"ana":    hoursAgo(1),
			"skewed": testNow.Add(6 * time.Hour),
		}},
		lastSeenConfig(48), "")

	assert.Equal(t, "skewed", svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
	assert.Equal(t, "ana", svc.EnsureAssignment("entry-2", "whatsapp", "phone-1"))
	assert.Equal(t, "skewed", svc.EnsureAssignment("entry-3", "whatsapp", "phone-1"))
}

func TestLastSeen_DepartedPointerResumesAtTheHead(t *testing.T) {
	repo := newStatefulRepo()
	repo.rrStates[rrKey("ws-1", "phone-1", "")] = &ia.RoundRobinState{
		WorkspaceID:        "ws-1",
		BusinessPhoneID:    "phone-1",
		LastAssignedUserID: "gone",
	}
	svc := lastSeenService(repo,
		defaultEligible(),
		&stubRoster{members: []string{"ana", "bob"}},
		&stubLastSeen{seen: map[string]time.Time{
			"ana": hoursAgo(5),
			"bob": hoursAgo(1),
		}},
		lastSeenConfig(48), "")

	assert.Equal(t, "bob", svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"),
		"the most recently online member is the head of the ring")
}

func TestLastSeen_SingleMemberRing(t *testing.T) {
	repo := newStatefulRepo()
	svc := lastSeenService(repo,
		defaultEligible(),
		&stubRoster{members: []string{"solo"}},
		&stubLastSeen{seen: map[string]time.Time{"solo": hoursAgo(2)}},
		lastSeenConfig(48), "")

	for i := 0; i < 3; i++ {
		assert.Equal(t, "solo", svc.EnsureAssignment(fmt.Sprintf("entry-%d", i), "whatsapp", "phone-1"))
	}
}

func TestLastSeen_AlreadyAssignedShortCircuits(t *testing.T) {
	repo := newStatefulRepo()
	repo.assignments[assignmentKey("ws-1", "entry-1", "whatsapp")] = &ia.InboxAssignment{AssignedUserID: "owner"}
	roster := &stubRoster{members: []string{"ana"}}

	svc := lastSeenService(repo, defaultEligible(), roster,
		&stubLastSeen{seen: map[string]time.Time{"ana": hoursAgo(1)}},
		lastSeenConfig(48), "")

	assert.Equal(t, "owner", svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
	assert.Empty(t, roster.calls, "the roster must not be consulted for an entry that already has an owner")
}

func TestLastSeen_DistributionIsEven(t *testing.T) {
	repo := newStatefulRepo()
	svc := lastSeenService(repo,
		defaultEligible(),
		&stubRoster{members: []string{"ana", "bob", "cid"}},
		&stubLastSeen{seen: map[string]time.Time{
			"ana": hoursAgo(1),
			"bob": hoursAgo(2),
			"cid": hoursAgo(3),
		}},
		lastSeenConfig(48), "")

	counts := map[string]int{}
	for i := 0; i < 30; i++ {
		counts[svc.EnsureAssignment(fmt.Sprintf("entry-%d", i), "whatsapp", "phone-1")]++
	}
	assert.Equal(t, map[string]int{"ana": 10, "bob": 10, "cid": 10}, counts)
}

func TestLastSeen_BehavesIdenticallyAcrossChannels(t *testing.T) {
	for _, entryType := range []string{"whatsapp", "unofficial_whatsapp", "telegram", "instagram"} {
		t.Run(entryType, func(t *testing.T) {
			svc := lastSeenService(newStatefulRepo(),
				defaultEligible(),
				&stubRoster{members: []string{"ana", "bob"}},
				&stubLastSeen{seen: map[string]time.Time{
					"ana": hoursAgo(1),
					"bob": hoursAgo(9),
				}},
				lastSeenConfig(48), "")

			assert.Equal(t, "ana", svc.EnsureAssignment("entry-1", entryType, "acct-1"))
			assert.Equal(t, "bob", svc.EnsureAssignment("entry-2", entryType, "acct-1"))
		})
	}
}
