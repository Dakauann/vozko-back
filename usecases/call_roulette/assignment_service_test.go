package call_roulette_usecase

import (
	"errors"
	"sort"
	"testing"

	"vozko/domain/call_roulette"
)

type mockState struct {
	workspaceID  string
	sipTrunkID   string
	departmentID string
	lastUser     string
}

type mockAssignment struct {
	entryID        string
	entryType      call_roulette.EntryType
	workspaceID    string
	sipTrunkID     string
	departmentID   string
	assignedUserID string
}

type mockRepo struct {
	states      []mockState
	assignments []mockAssignment
}

func (m *mockRepo) GetState(workspaceID, sipTrunkID, departmentID string) (*call_roulette.RouletteState, error) {
	for _, s := range m.states {
		if s.workspaceID == workspaceID && s.sipTrunkID == sipTrunkID && s.departmentID == departmentID {
			return &call_roulette.RouletteState{
				WorkspaceID:        s.workspaceID,
				SIPTrunkID:         s.sipTrunkID,
				DepartmentID:       s.departmentID,
				LastAssignedUserID: s.lastUser,
			}, nil
		}
	}
	return nil, nil
}

func (m *mockRepo) SaveState(state *call_roulette.RouletteState) error {
	for i, s := range m.states {
		if s.workspaceID == state.WorkspaceID && s.sipTrunkID == state.SIPTrunkID && s.departmentID == state.DepartmentID {
			m.states[i].lastUser = state.LastAssignedUserID
			return nil
		}
	}
	m.states = append(m.states, mockState{
		workspaceID:  state.WorkspaceID,
		sipTrunkID:   state.SIPTrunkID,
		departmentID: state.DepartmentID,
		lastUser:     state.LastAssignedUserID,
	})
	return nil
}

func (m *mockRepo) Assign(entryID string, entryType call_roulette.EntryType, workspaceID, sipTrunkID, departmentID, assignedUserID string) error {
	m.assignments = append(m.assignments, mockAssignment{
		entryID:        entryID,
		entryType:      entryType,
		workspaceID:    workspaceID,
		sipTrunkID:     sipTrunkID,
		departmentID:   departmentID,
		assignedUserID: assignedUserID,
	})
	return nil
}

func (m *mockRepo) FindByEntry(workspaceID, entryID string, entryType call_roulette.EntryType) (*call_roulette.CallAssignment, error) {
	for _, a := range m.assignments {
		if a.entryID == entryID && a.workspaceID == workspaceID {
			return &call_roulette.CallAssignment{AssignedUserID: a.assignedUserID}, nil
		}
	}
	return nil, nil
}

func (m *mockRepo) Unassign(workspaceID, entryID string, entryType call_roulette.EntryType) error {
	var filtered []mockAssignment
	for _, a := range m.assignments {
		if !(a.entryID == entryID && a.workspaceID == workspaceID) {
			filtered = append(filtered, a)
		}
	}
	m.assignments = filtered
	return nil
}

func (m *mockRepo) ListByUser(workspaceID, userID string) ([]*call_roulette.CallAssignment, error) {
	var result []*call_roulette.CallAssignment
	for _, a := range m.assignments {
		if a.assignedUserID == userID && a.workspaceID == workspaceID {
			result = append(result, &call_roulette.CallAssignment{AssignedUserID: a.assignedUserID})
		}
	}
	return result, nil
}

type mockEligibleUsers struct {
	byWorkspace     map[string][]string
	byWorkspaceDept map[string]map[string][]string
}

func (m *mockEligibleUsers) GetEligibleUsersForWorkspace(workspaceID string, _ bool) []string {
	return m.byWorkspace[workspaceID]
}

func (m *mockEligibleUsers) GetEligibleUsersForWorkspaceDepartment(workspaceID, departmentID string, _ bool) []string {
	if dept, ok := m.byWorkspaceDept[workspaceID]; ok {
		return dept[departmentID]
	}
	return nil
}

type mockCampaignResolver struct {
	workspaceIDs  map[string]string
	departmentIDs map[string]string
	sipTrunkIDs   map[string]string
}

func (m *mockCampaignResolver) GetEntryWorkspaceID(entryID string) (string, error) {
	if ws, ok := m.workspaceIDs[entryID]; ok {
		return ws, nil
	}
	return "", errors.New("not found")
}
func (m *mockCampaignResolver) GetEntryDepartmentID(entryID string) (string, error) {
	return m.departmentIDs[entryID], nil
}
func (m *mockCampaignResolver) GetEntrySIPTrunkID(entryID string) (string, error) {
	if t, ok := m.sipTrunkIDs[entryID]; ok {
		return t, nil
	}
	return "", errors.New("not found")
}

type mockConfigProvider struct {
	skip bool
}

func (m *mockConfigProvider) GetByWorkspaceID(workspaceID string) (bool, error) {
	return m.skip, nil
}

func makeUsers(names ...string) []string {
	sorted := make([]string, len(names))
	copy(sorted, names)
	sort.Strings(sorted)
	return sorted
}

func TestAssignsToFirstUserWhenNoState(t *testing.T) {
	repo := &mockRepo{}
	users := &mockEligibleUsers{
		byWorkspace: map[string][]string{"ws1": makeUsers("alice", "bob", "carol")},
	}
	resolver := &mockCampaignResolver{
		workspaceIDs:  map[string]string{"entry1": "ws1"},
		departmentIDs: map[string]string{},
		sipTrunkIDs:   map[string]string{"entry1": "trunk1"},
	}

	svc := NewAssignmentService(repo, users, resolver, &mockConfigProvider{})
	assigned := svc.EnsureAssignment("entry1", "trunk1")

	if assigned == "" {
		t.Fatal("expected an assignment, got empty")
	}

	if assigned != "alice" {
		t.Fatalf("expected 'alice' (first alphabetically), got '%s'", assigned)
	}
	if len(repo.assignments) != 1 {
		t.Fatalf("expected 1 assignment, got %d", len(repo.assignments))
	}
	if len(repo.states) != 1 {
		t.Fatalf("expected 1 state row, got %d", len(repo.states))
	}
	if repo.states[0].lastUser != "alice" {
		t.Fatalf("expected state.lastUser='alice', got '%s'", repo.states[0].lastUser)
	}
}

func TestRoundRobinRotatesThroughUsers(t *testing.T) {
	repo := &mockRepo{}
	users := &mockEligibleUsers{
		byWorkspace: map[string][]string{"ws1": makeUsers("alice", "bob", "carol")},
	}
	resolver := &mockCampaignResolver{
		workspaceIDs:  map[string]string{"entry1": "ws1", "entry2": "ws1", "entry3": "ws1"},
		departmentIDs: map[string]string{},
		sipTrunkIDs:   map[string]string{"entry1": "trunk1", "entry2": "trunk1", "entry3": "trunk1"},
	}

	svc := NewAssignmentService(repo, users, resolver, &mockConfigProvider{})

	a1 := svc.EnsureAssignment("entry1", "trunk1")
	a2 := svc.EnsureAssignment("entry2", "trunk1")
	a3 := svc.EnsureAssignment("entry3", "trunk1")

	if a1 != "alice" || a2 != "bob" || a3 != "carol" {
		t.Fatalf("expected alice→bob→carol, got %s→%s→%s", a1, a2, a3)
	}
}

func TestRoundRobinWrapsAround(t *testing.T) {
	repo := &mockRepo{}
	users := &mockEligibleUsers{
		byWorkspace: map[string][]string{"ws1": makeUsers("alice", "bob")},
	}
	resolver := &mockCampaignResolver{
		workspaceIDs:  map[string]string{"e1": "ws1", "e2": "ws1", "e3": "ws1", "e4": "ws1"},
		departmentIDs: map[string]string{},
		sipTrunkIDs:   map[string]string{"e1": "t1", "e2": "t1", "e3": "t1", "e4": "t1"},
	}

	svc := NewAssignmentService(repo, users, resolver, &mockConfigProvider{})

	a1 := svc.EnsureAssignment("e1", "t1")
	a2 := svc.EnsureAssignment("e2", "t1")
	a3 := svc.EnsureAssignment("e3", "t1")
	a4 := svc.EnsureAssignment("e4", "t1")

	if a1 != "alice" || a2 != "bob" || a3 != "alice" || a4 != "bob" {
		t.Fatalf("expected alice→bob→alice→bob, got %s→%s→%s→%s", a1, a2, a3, a4)
	}
}

func TestExistingAssignmentReturnedWithoutNewRound(t *testing.T) {
	repo := &mockRepo{}
	users := &mockEligibleUsers{
		byWorkspace: map[string][]string{"ws1": makeUsers("alice", "bob")},
	}
	resolver := &mockCampaignResolver{
		workspaceIDs:  map[string]string{"e1": "ws1"},
		departmentIDs: map[string]string{},
		sipTrunkIDs:   map[string]string{"e1": "t1"},
	}

	svc := NewAssignmentService(repo, users, resolver, &mockConfigProvider{})

	a1 := svc.EnsureAssignment("e1", "t1")
	before := len(repo.assignments)
	a2 := svc.EnsureAssignment("e1", "t1")

	if a1 != a2 {
		t.Fatalf("expected same assignment on second call, got %s vs %s", a1, a2)
	}
	if len(repo.assignments) != before {
		t.Fatalf("expected no new assignment on second call, got %d (was %d)", len(repo.assignments), before)
	}
}

func TestNoAssignmentWhenNoUsersConnected(t *testing.T) {
	repo := &mockRepo{}
	users := &mockEligibleUsers{
		byWorkspace: map[string][]string{"ws1": {}},
	}
	resolver := &mockCampaignResolver{
		workspaceIDs: map[string]string{"e1": "ws1"},
	}
	svc := NewAssignmentService(repo, users, resolver, &mockConfigProvider{})

	assigned := svc.EnsureAssignment("e1", "t1")
	if assigned != "" {
		t.Fatalf("expected no assignment, got %s", assigned)
	}
}

func TestSkipsDisconnectedUserInRoundRobin(t *testing.T) {
	repo := &mockRepo{}
	users := &mockEligibleUsers{

		byWorkspace: map[string][]string{"ws1": makeUsers("bob", "carol")},
	}
	resolver := &mockCampaignResolver{
		workspaceIDs:  map[string]string{"e1": "ws1", "e2": "ws1", "e3": "ws1"},
		departmentIDs: map[string]string{},
		sipTrunkIDs:   map[string]string{"e1": "t1", "e2": "t1", "e3": "t1"},
	}

	svc := NewAssignmentService(repo, users, resolver, &mockConfigProvider{})

	a1 := svc.EnsureAssignment("e1", "t1")
	a2 := svc.EnsureAssignment("e2", "t1")
	a3 := svc.EnsureAssignment("e3", "t1")

	if a1 != "bob" || a2 != "carol" || a3 != "bob" {
		t.Fatalf("expected bob→carol→bob, got %s→%s→%s", a1, a2, a3)
	}
}

func TestContinuesAfterDisconnectedLastUser(t *testing.T) {
	repo := &mockRepo{

		states: []mockState{{
			workspaceID: "ws1", sipTrunkID: "t1", departmentID: "",
			lastUser: "alice",
		}},
	}

	users := &mockEligibleUsers{
		byWorkspace: map[string][]string{"ws1": makeUsers("bob")},
	}
	resolver := &mockCampaignResolver{
		workspaceIDs:  map[string]string{"e1": "ws1"},
		departmentIDs: map[string]string{},
		sipTrunkIDs:   map[string]string{"e1": "t1"},
	}
	svc := NewAssignmentService(repo, users, resolver, &mockConfigProvider{})
	assigned := svc.EnsureAssignment("e1", "t1")
	if assigned != "bob" {
		t.Fatalf("expected 'bob' (only connected user), got '%s'", assigned)
	}
}

func TestRoundRobinIsPerTrunkIndependent(t *testing.T) {
	repo := &mockRepo{}
	users := &mockEligibleUsers{
		byWorkspace: map[string][]string{
			"ws1": makeUsers("alice", "bob", "carol"),
		},
	}
	resolver := &mockCampaignResolver{
		workspaceIDs:  map[string]string{"e1": "ws1", "e2": "ws1"},
		departmentIDs: map[string]string{},
		sipTrunkIDs:   map[string]string{"e1": "trunkA", "e2": "trunkB"},
	}
	svc := NewAssignmentService(repo, users, resolver, &mockConfigProvider{})

	a1 := svc.EnsureAssignment("e1", "trunkA")
	a2 := svc.EnsureAssignment("e2", "trunkB")

	if a1 != "alice" || a2 != "alice" {
		t.Fatalf("expected both trunks to start from alice, got %s and %s", a1, a2)
	}
}

func TestDepartmentScopedAssignment(t *testing.T) {
	repo := &mockRepo{}
	users := &mockEligibleUsers{
		byWorkspaceDept: map[string]map[string][]string{
			"ws1": {"deptA": makeUsers("alice", "bob"), "deptB": makeUsers("carol")},
		},
	}
	resolver := &mockCampaignResolver{
		workspaceIDs:  map[string]string{"e1": "ws1"},
		departmentIDs: map[string]string{"e1": "deptA"},
		sipTrunkIDs:   map[string]string{"e1": "t1"},
	}
	svc := NewAssignmentService(repo, users, resolver, &mockConfigProvider{})

	a1 := svc.EnsureAssignment("e1", "t1")
	if a1 != "alice" {
		t.Fatalf("expected 'alice' from deptA, got '%s'", a1)
	}
}

func TestUnassignRemovesAssignment(t *testing.T) {
	repo := &mockRepo{}
	users := &mockEligibleUsers{
		byWorkspace: map[string][]string{"ws1": makeUsers("alice")},
	}
	resolver := &mockCampaignResolver{
		workspaceIDs:  map[string]string{"e1": "ws1"},
		departmentIDs: map[string]string{},
		sipTrunkIDs:   map[string]string{"e1": "t1"},
	}
	svc := NewAssignmentService(repo, users, resolver, &mockConfigProvider{})

	svc.EnsureAssignment("e1", "t1")
	svc.Unassign("ws1", "e1")

	assigned := svc.GetAssignedUserID("ws1", "e1")
	if assigned != "" {
		t.Fatalf("expected empty after unassign, got '%s'", assigned)
	}
}

func TestListByUserReturnsAssignments(t *testing.T) {
	repo := &mockRepo{}
	users := &mockEligibleUsers{
		byWorkspace: map[string][]string{"ws1": makeUsers("alice", "bob")},
	}
	resolver := &mockCampaignResolver{
		workspaceIDs:  map[string]string{"e1": "ws1", "e2": "ws1"},
		departmentIDs: map[string]string{},
		sipTrunkIDs:   map[string]string{"e1": "t1", "e2": "t1"},
	}
	svc := NewAssignmentService(repo, users, resolver, &mockConfigProvider{})

	svc.EnsureAssignment("e1", "t1")
	svc.EnsureAssignment("e2", "t1")

	aliceCalls := svc.ListByUser("ws1", "alice")
	if len(aliceCalls) != 1 {
		t.Fatalf("expected 1 assignment for alice, got %d", len(aliceCalls))
	}

	bobCalls := svc.ListByUser("ws1", "bob")
	if len(bobCalls) != 1 {
		t.Fatalf("expected 1 assignment for bob, got %d", len(bobCalls))
	}
}
