package inbox_assignment_usecase

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ia "vozko/domain/inbox_assignment"
)

type racyRepo struct {
	*statefulRepo

	mu     sync.Mutex
	reads  int
	writes int

	beforeSwap func(read int)
	swapErr    error
}

func newRacyRepo() *racyRepo { return &racyRepo{statefulRepo: newStatefulRepo()} }

func (r *racyRepo) GetRoundRobinState(wsID, phoneID, deptID string) (*ia.RoundRobinState, error) {
	r.mu.Lock()
	r.reads++
	n := r.reads
	state := r.rrStates[rrKey(wsID, phoneID, deptID)]
	var cp *ia.RoundRobinState
	if state != nil {
		v := *state
		cp = &v
	}
	r.mu.Unlock()

	if r.beforeSwap != nil {
		r.beforeSwap(n)
	}
	return cp, nil
}

func (r *racyRepo) CompareAndSwapRoundRobinState(state *ia.RoundRobinState, expected string) (bool, error) {
	if r.swapErr != nil {
		return false, r.swapErr
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.writes++
	key := rrKey(state.WorkspaceID, state.BusinessPhoneID, state.DepartmentID)
	current := ""
	if existing := r.rrStates[key]; existing != nil {
		current = existing.LastAssignedUserID
	}
	if current != expected {
		return false, nil
	}
	cp := *state
	r.rrStates[key] = &cp
	return true, nil
}

func (r *racyRepo) Assign(a *ia.InboxAssignment) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *a
	r.assignments[assignmentKey(a.WorkspaceID, a.EntryID, a.EntryType)] = &cp
	return nil
}

func (r *racyRepo) FindByEntry(wsID, entryID, entryType string) (*ia.InboxAssignment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.assignments[assignmentKey(wsID, entryID, entryType)], nil
}

func TestRoundRobin_ConcurrentAssignmentsDoNotDrawTheSameAgent(t *testing.T) {
	repo := newRacyRepo()

	firstHasRead := make(chan struct{})
	secondHasRead := make(chan struct{})
	var once sync.Once

	repo.beforeSwap = func(read int) {
		if read == 1 {
			close(firstHasRead)
			<-secondHasRead
			return
		}
		if read == 2 {
			once.Do(func() { close(secondHasRead) })
		}
	}

	svc := lastSeenService(repo.statefulRepo, defaultEligible(),
		&stubRoster{members: []string{"ana", "bob"}},
		&stubLastSeen{seen: map[string]time.Time{"ana": hoursAgo(1), "bob": hoursAgo(2)}},
		lastSeenConfig(48), "")
	svc.repo = repo

	var wg sync.WaitGroup
	owners := make([]string, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-firstHasRead
		owners[1] = svc.EnsureAssignment("entry-2", "whatsapp", "phone-1")
	}()
	go func() {
		defer wg.Done()
		owners[0] = svc.EnsureAssignment("entry-1", "whatsapp", "phone-1")
	}()
	wg.Wait()

	require.NotEmpty(t, owners[0])
	require.NotEmpty(t, owners[1])
	assert.NotEqual(t, owners[0], owners[1],
		"two conversations racing on the same pointer must reach two different agents")
	assert.Greater(t, repo.reads, 2, "the loser must have re-read the pointer that won")
}

func TestRoundRobin_SwapErrorStillAssigns(t *testing.T) {
	repo := newRacyRepo()
	repo.swapErr = errors.New("pointer write failed")

	svc := lastSeenService(repo.statefulRepo, defaultEligible(),
		&stubRoster{members: []string{"ana"}},
		&stubLastSeen{seen: map[string]time.Time{"ana": hoursAgo(1)}},
		lastSeenConfig(48), "")
	svc.repo = repo

	assert.Equal(t, "ana", svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"))
}

func TestRoundRobin_UnresolvedContentionStillAssigns(t *testing.T) {
	repo := newRacyRepo()
	repo.beforeSwap = func(read int) {
		repo.mu.Lock()
		repo.rrStates[rrKey("ws-1", "phone-1", "")] = &ia.RoundRobinState{
			WorkspaceID:        "ws-1",
			BusinessPhoneID:    "phone-1",
			LastAssignedUserID: fmt.Sprintf("interloper-%d", read),
		}
		repo.mu.Unlock()
	}

	svc := lastSeenService(repo.statefulRepo, defaultEligible(),
		&stubRoster{members: []string{"ana", "bob"}},
		&stubLastSeen{seen: map[string]time.Time{"ana": hoursAgo(1), "bob": hoursAgo(2)}},
		lastSeenConfig(48), "")
	svc.repo = repo

	assert.NotEmpty(t, svc.EnsureAssignment("entry-1", "whatsapp", "phone-1"),
		"contention must never cost the conversation its owner")
	assert.Equal(t, maxRoundRobinAttempts, repo.writes, "every attempt must have been tried")
}
