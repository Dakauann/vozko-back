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

// rouletteConfig is a workspace-config provider that can express the whole
// roulette policy, unlike mockWorkspaceConfig which only carries skipAdmins.
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
	calls   []string // "workspaceID|departmentID|skipAdmins"
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

// lastSeenService wires a service in last_seen mode with a frozen clock.
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

// The mode's headline behaviour: offline members receive conversations, ordered
// by how recently they were online.
func TestLastSeen_AssignsToOfflineMembersByRecency(t *testing.T) {
	repo := newStatefulRepo()
	svc := lastSeenService(repo,
		defaultEligible(), // nobody connected at all
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

// E15: past the window is out of the ring — the requester's "acima de 1/2 dias
// offline não entra na roleta".
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

// E15 continued: everybody stale and nobody connected leaves the entry
// unassigned, which is the feature working rather than a failure.
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

// E16: a connected member is always at the head, regardless of how stale their
// presence row is — the online overlay beats the presence table.
func TestLastSeen_ConnectedMemberSortsToTheHead(t *testing.T) {
	repo := newStatefulRepo()
	svc := lastSeenService(repo,
		defaultEligible("stale"), // connected right now
		&stubRoster{members: []string{"ana", "stale"}},
		&stubLastSeen{seen: map[string]time.Time{
			"ana":   hoursAgo(1),
			"stale": hoursAgo(200), // ancient row, but they are online
		}},
		lastSeenConfig(48), "")

	assert.Equal(t, "stale", svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
	assert.Equal(t, "ana", svc.EnsureAssignment("entry-2", "whatsapp", "phone-1"))
}

// E25: the window boundary, through the whole service.
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

// E8: the window is read per assignment, so an admin's change takes effect
// immediately.
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

// E12: nobody has a presence record at all — a fresh deploy, or a broken
// telemetry consumer. Falling back to the online pool is today's behaviour and
// therefore cannot be a regression; leaving a whole workspace unassigned would
// be an outage.
func TestLastSeen_NoPresenceDataFallsBackToTheOnlinePool(t *testing.T) {
	repo := newStatefulRepo()
	svc := lastSeenService(repo,
		defaultEligible("connected"),
		&stubRoster{members: []string{"ana", "bob", "connected"}},
		&stubLastSeen{seen: map[string]time.Time{}},
		lastSeenConfig(48), "")

	// "connected" is online, so it survives the ring on its own; the fallback
	// is exercised by the all-offline variant below.
	assert.Equal(t, "connected", svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))

	offline := lastSeenService(newStatefulRepo(),
		defaultEligible("connected"),
		&stubRoster{members: []string{"ana", "bob"}}, // roster excludes the connected user
		&stubLastSeen{seen: map[string]time.Time{}},
		lastSeenConfig(48), "")
	assert.Equal(t, "connected", offline.EnsureAssignment("entry-1", "whatsapp", "phone-1"),
		"an empty last-seen ring must fall back to the connected pool")
}

// E13/E14: a failing read degrades to the online pool, never to no assignment.
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

// A workspace asking for last_seen before the readers are wired must keep
// distributing, not stop.
func TestLastSeen_UnwiredReadersFallBackToTheOnlinePool(t *testing.T) {
	svc := lastSeenService(newStatefulRepo(),
		defaultEligible("connected"),
		nil, nil,
		lastSeenConfig(48), "")
	assert.Equal(t, "connected", svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
}

// E13 corollary: the config read itself failing must not switch anyone into a
// half-configured mode.
func TestLastSeen_ConfigErrorUsesTheOnlinePool(t *testing.T) {
	svc := lastSeenService(newStatefulRepo(),
		defaultEligible("connected"),
		&stubRoster{members: []string{"ana"}},
		&stubLastSeen{seen: map[string]time.Time{"ana": hoursAgo(1)}},
		&rouletteConfig{err: errors.New("boom")}, "")
	assert.Equal(t, "connected", svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
}

// E3: an unknown mode in the database resolves to the historical behaviour.
func TestLastSeen_UnknownModeUsesTheOnlinePool(t *testing.T) {
	svc := lastSeenService(newStatefulRepo(),
		defaultEligible("connected"),
		&stubRoster{members: []string{"ana"}},
		&stubLastSeen{seen: map[string]time.Time{"ana": hoursAgo(1)}},
		&rouletteConfig{cfg: &wsc.WorkspaceConfig{RouletteMode: "garbage"}}, "")
	assert.Equal(t, "connected", svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
}

// E19: department narrowing reaches the roster, and skipAdmins with it.
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

// E22: a presence row for somebody who left the workspace must not put them
// back in the ring — the roster is the population, presence is only the order.
func TestLastSeen_PresenceForNonMembersIsIgnored(t *testing.T) {
	svc := lastSeenService(newStatefulRepo(),
		defaultEligible(),
		&stubRoster{members: []string{"ana"}},
		&stubLastSeen{seen: map[string]time.Time{
			"ana":    hoursAgo(10),
			"leaver": hoursAgo(1), // more recent, but no longer a member
		}},
		lastSeenConfig(48), "")

	assert.Equal(t, "ana", svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
}

// E23: a skewed clock must not pin one agent to the head forever.
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

	// Clamped to now, so it still leads — but the ring rotates normally rather
	// than the pointer never advancing past it.
	assert.Equal(t, "skewed", svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
	assert.Equal(t, "ana", svc.EnsureAssignment("entry-2", "whatsapp", "phone-1"))
	assert.Equal(t, "skewed", svc.EnsureAssignment("entry-3", "whatsapp", "phone-1"))
}

// E30: the round-robin pointer naming somebody who has aged out resumes at the
// head, which in this ring is the most recently online member.
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

// E32: a ring of one keeps assigning to that one member.
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

// E33: an already-assigned entry never reaches the pool at all, in either mode.
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

// Fairness: over a full number of laps every ring member receives exactly the
// same number of conversations. This is the property that makes it a roulette
// rather than a preference.
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

// E57: the mode is channel-agnostic because there is one code path. Same
// roster, same presence, different entry types.
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
