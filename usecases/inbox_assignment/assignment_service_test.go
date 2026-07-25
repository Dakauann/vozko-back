package inbox_assignment_usecase

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	conversation "vozko/domain/conversation"
	ia "vozko/domain/inbox_assignment"
	wsc "vozko/domain/workspace_config"
)

type mockRepo struct {
	findByEntryResult *ia.InboxAssignment
	findByEntryErr    error

	isAssignedResult bool
	isAssignedErr    error

	assignErr   error
	assigned    *ia.InboxAssignment
	assignCalls []*ia.InboxAssignment

	roundRobin    *ia.RoundRobinState
	roundRobinErr error

	saveStateErr error
	savedStates  []*ia.RoundRobinState
}

func (r *mockRepo) FindByEntry(string, string, string) (*ia.InboxAssignment, error) {
	return r.findByEntryResult, r.findByEntryErr
}
func (r *mockRepo) FindByEntries(string, []string) ([]*ia.InboxAssignment, error) {
	return nil, nil
}
func (r *mockRepo) FindByEntryAndUser(string, string, string, string) (*ia.InboxAssignment, error) {
	return nil, nil
}
func (r *mockRepo) Assign(a *ia.InboxAssignment) error {
	if r.assignErr != nil {
		return r.assignErr
	}
	cp := *a
	r.assigned = &cp
	r.assignCalls = append(r.assignCalls, &cp)
	return nil
}
func (r *mockRepo) Unassign(string, string, string) error { return nil }
func (r *mockRepo) ListByUser(string, string, string) ([]string, error) {
	return nil, nil
}
func (r *mockRepo) IsAssignedToUser(string, string, string, string) (bool, error) {
	return r.isAssignedResult, r.isAssignedErr
}
func (r *mockRepo) GetRoundRobinState(string, string, string) (*ia.RoundRobinState, error) {
	return r.roundRobin, r.roundRobinErr
}
func (r *mockRepo) SaveRoundRobinState(state *ia.RoundRobinState) error {
	if r.saveStateErr != nil {
		return r.saveStateErr
	}
	cp := *state
	r.roundRobin = &cp
	r.savedStates = append(r.savedStates, &cp)
	return nil
}

type statefulRepo struct {
	assignments map[string]*ia.InboxAssignment
	rrStates    map[string]*ia.RoundRobinState
}

func newStatefulRepo() *statefulRepo {
	return &statefulRepo{
		assignments: make(map[string]*ia.InboxAssignment),
		rrStates:    make(map[string]*ia.RoundRobinState),
	}
}

func assignmentKey(wsID, entryID, entryType string) string {
	return wsID + "|" + entryID + "|" + entryType
}
func rrKey(wsID, phoneID, deptID string) string { return wsID + "|" + phoneID + "|" + deptID }

func (r *statefulRepo) FindByEntry(wsID, entryID, entryType string) (*ia.InboxAssignment, error) {
	return r.assignments[assignmentKey(wsID, entryID, entryType)], nil
}
func (r *statefulRepo) FindByEntries(string, []string) ([]*ia.InboxAssignment, error) {
	return nil, nil
}
func (r *statefulRepo) FindByEntryAndUser(string, string, string, string) (*ia.InboxAssignment, error) {
	return nil, nil
}
func (r *statefulRepo) Assign(a *ia.InboxAssignment) error {
	cp := *a
	r.assignments[assignmentKey(a.WorkspaceID, a.EntryID, a.EntryType)] = &cp
	return nil
}
func (r *statefulRepo) Unassign(wsID, entryID, entryType string) error {
	delete(r.assignments, assignmentKey(wsID, entryID, entryType))
	return nil
}
func (r *statefulRepo) ListByUser(string, string, string) ([]string, error) {
	return nil, nil
}
func (r *statefulRepo) IsAssignedToUser(wsID, entryID, entryType, userID string) (bool, error) {
	a := r.assignments[assignmentKey(wsID, entryID, entryType)]
	if a == nil {
		return false, nil
	}
	return a.AssignedUserID == userID, nil
}
func (r *statefulRepo) GetRoundRobinState(wsID, phoneID, deptID string) (*ia.RoundRobinState, error) {
	return r.rrStates[rrKey(wsID, phoneID, deptID)], nil
}
func (r *statefulRepo) SaveRoundRobinState(state *ia.RoundRobinState) error {
	cp := *state
	r.rrStates[rrKey(state.WorkspaceID, state.BusinessPhoneID, state.DepartmentID)] = &cp
	return nil
}

type mockEligible struct {
	workspaceUsers  []string
	departmentUsers map[string][]string

	workspaceCalls  []eligibleWorkspaceCall
	departmentCalls []eligibleDeptCall
}

type eligibleWorkspaceCall struct {
	workspaceID string
	skipAdmins  bool
}

type eligibleDeptCall struct {
	workspaceID  string
	departmentID string
	skipAdmins   bool
}

func (e *mockEligible) GetEligibleUsersForWorkspace(wsID string, skip bool) []string {
	e.workspaceCalls = append(e.workspaceCalls, eligibleWorkspaceCall{wsID, skip})
	return append([]string(nil), e.workspaceUsers...)
}

func (e *mockEligible) GetEligibleUsersForWorkspaceDepartment(wsID, deptID string, skip bool) []string {
	e.departmentCalls = append(e.departmentCalls, eligibleDeptCall{wsID, deptID, skip})
	key := wsID + ":" + deptID
	return append([]string(nil), e.departmentUsers[key]...)
}

type dynamicEligible struct {
	callNum int
	pools   [][]string
}

func (d *dynamicEligible) GetEligibleUsersForWorkspace(string, bool) []string {
	idx := d.callNum
	if idx >= len(d.pools) {
		idx = len(d.pools) - 1
	}
	d.callNum++
	return append([]string(nil), d.pools[idx]...)
}

func (d *dynamicEligible) GetEligibleUsersForWorkspaceDepartment(string, string, bool) []string {
	return d.GetEligibleUsersForWorkspace("", false)
}

type mockResolver struct {
	workspaceID   string
	workspaceErr  error
	departmentID  string
	departmentErr error
}

func (r *mockResolver) GetCampaignWorkspaceID(string, string) (string, error) {
	return r.workspaceID, r.workspaceErr
}
func (r *mockResolver) GetCampaignDepartmentID(string, string) (string, error) {
	return r.departmentID, r.departmentErr
}
func (r *mockResolver) GetEntryWorkspaceID(string, string) (string, error) {
	return r.workspaceID, r.workspaceErr
}
func (r *mockResolver) GetEntryDepartmentID(string, string) (string, error) {
	return r.departmentID, r.departmentErr
}
func (r *mockResolver) GetEntryCampaignID(string, string) (string, error) {
	return "campaign-1", nil
}

var _ conversation.CampaignWorkspaceResolver = (*mockResolver)(nil)

type mockWorkspaceConfig struct {
	skipAdmins bool
	err        error
	nilCfg     bool
}

func (c *mockWorkspaceConfig) GetByWorkspaceID(context.Context, string) (*wsc.WorkspaceConfig, error) {
	if c.err != nil {
		return nil, c.err
	}
	if c.nilCfg {
		return nil, nil
	}
	return &wsc.WorkspaceConfig{SkipAdminAssignment: c.skipAdmins}, nil
}

func newService(repo ia.Repository, eligible conversation.EligibleUserProvider, resolver conversation.CampaignWorkspaceResolver, cfg WorkspaceConfigProvider) *AssignmentService {
	return NewAssignmentService(repo, eligible, resolver, cfg)
}

func defaultEligible(users ...string) *mockEligible {
	return &mockEligible{workspaceUsers: users}
}

func deptEligible(wsUsers []string, deptKey string, deptUsers []string) *mockEligible {
	return &mockEligible{
		workspaceUsers:  wsUsers,
		departmentUsers: map[string][]string{deptKey: deptUsers},
	}
}

func defaultResolver(wsID, deptID string) *mockResolver {
	return &mockResolver{workspaceID: wsID, departmentID: deptID}
}

func defaultConfig() *mockWorkspaceConfig {
	return &mockWorkspaceConfig{}
}

func TestEnsureAssignment_WorkspaceResolutionError(t *testing.T) {
	repo := &mockRepo{}
	service := newService(repo, defaultEligible("u1"), &mockResolver{workspaceErr: errors.New("db down")}, defaultConfig())

	result := service.EnsureAssignment("entry-1", "whatsapp", "phone-1")

	require.Empty(t, result)
	require.Nil(t, repo.assigned)
}

func TestEnsureAssignment_WorkspaceResolutionEmpty(t *testing.T) {
	repo := &mockRepo{}
	service := newService(repo, defaultEligible("u1"), &mockResolver{workspaceID: ""}, defaultConfig())

	result := service.EnsureAssignment("entry-1", "whatsapp", "phone-1")

	require.Empty(t, result)
	require.Nil(t, repo.assigned)
}

func TestEnsureAssignment_FindByEntryError(t *testing.T) {
	repo := &mockRepo{findByEntryErr: errors.New("query failed")}
	service := newService(repo, defaultEligible("u1"), defaultResolver("ws-1", ""), defaultConfig())

	result := service.EnsureAssignment("entry-1", "whatsapp", "phone-1")

	require.Empty(t, result)
	require.Nil(t, repo.assigned)
}

func TestEnsureAssignment_AlreadyAssigned(t *testing.T) {
	repo := &mockRepo{
		findByEntryResult: &ia.InboxAssignment{AssignedUserID: "existing-user"},
	}
	service := newService(repo, defaultEligible("u1"), defaultResolver("ws-1", ""), defaultConfig())

	result := service.EnsureAssignment("entry-1", "whatsapp", "phone-1")

	require.Equal(t, "existing-user", result)
	require.Nil(t, repo.assigned, "should not create a new assignment")
}

func TestEnsureAssignment_NilWorkspaceConfigProvider(t *testing.T) {
	eligible := defaultEligible("user-a")
	service := newService(&mockRepo{}, eligible, defaultResolver("ws-1", ""), nil)

	result := service.EnsureAssignment("entry-1", "whatsapp", "phone-1")

	require.Equal(t, "user-a", result)

	require.Len(t, eligible.workspaceCalls, 1)
	require.False(t, eligible.workspaceCalls[0].skipAdmins)
}

func TestEnsureAssignment_WorkspaceConfigError(t *testing.T) {
	eligible := defaultEligible("user-a")
	cfg := &mockWorkspaceConfig{err: errors.New("config db error")}
	service := newService(&mockRepo{}, eligible, defaultResolver("ws-1", ""), cfg)

	result := service.EnsureAssignment("entry-1", "whatsapp", "phone-1")

	require.Equal(t, "user-a", result)
	require.False(t, eligible.workspaceCalls[0].skipAdmins)
}

func TestEnsureAssignment_WorkspaceConfigNilCfg(t *testing.T) {
	eligible := defaultEligible("user-a")
	cfg := &mockWorkspaceConfig{nilCfg: true}
	service := newService(&mockRepo{}, eligible, defaultResolver("ws-1", ""), cfg)

	result := service.EnsureAssignment("entry-1", "whatsapp", "phone-1")

	require.Equal(t, "user-a", result)
	require.False(t, eligible.workspaceCalls[0].skipAdmins)
}

func TestEnsureAssignment_SkipAdminsTrue(t *testing.T) {
	eligible := defaultEligible("user-a")
	cfg := &mockWorkspaceConfig{skipAdmins: true}
	service := newService(&mockRepo{}, eligible, defaultResolver("ws-1", ""), cfg)

	result := service.EnsureAssignment("entry-1", "whatsapp", "phone-1")

	require.Equal(t, "user-a", result)
	require.True(t, eligible.workspaceCalls[0].skipAdmins)
}

func TestEnsureAssignment_DepartmentResolutionError(t *testing.T) {
	resolver := &mockResolver{workspaceID: "ws-1", departmentErr: errors.New("dept query failed")}
	repo := &mockRepo{}
	service := newService(repo, defaultEligible("u1"), resolver, defaultConfig())

	result := service.EnsureAssignment("entry-1", "whatsapp", "phone-1")

	require.Empty(t, result)
	require.Nil(t, repo.assigned)
}

func TestEnsureAssignment_NoDepartment_UsesWorkspaceUsers(t *testing.T) {
	eligible := &mockEligible{
		workspaceUsers:  []string{"ws-user"},
		departmentUsers: map[string][]string{"ws-1:dept-1": {"dept-user"}},
	}
	repo := &mockRepo{}
	service := newService(repo, eligible, defaultResolver("ws-1", ""), defaultConfig())

	result := service.EnsureAssignment("entry-1", "whatsapp", "phone-1")

	require.Equal(t, "ws-user", result)
	require.Len(t, eligible.workspaceCalls, 1)
	require.Empty(t, eligible.departmentCalls)
}

func TestEnsureAssignment_WithDepartment_UsesDepartmentUsers(t *testing.T) {
	eligible := deptEligible([]string{"ws-user"}, "ws-1:dept-1", []string{"dept-user"})
	repo := &mockRepo{}
	service := newService(repo, eligible, defaultResolver("ws-1", "dept-1"), defaultConfig())

	result := service.EnsureAssignment("entry-1", "whatsapp", "phone-1")

	require.Equal(t, "dept-user", result)
	require.NotNil(t, repo.assigned)
	require.Equal(t, "dept-user", repo.assigned.AssignedUserID)
	require.Equal(t, "ws-1", repo.assigned.WorkspaceID)
}

func TestEnsureAssignment_NoConnectedUsers_NoDepartment(t *testing.T) {
	repo := &mockRepo{}
	service := newService(repo, defaultEligible(), defaultResolver("ws-1", ""), defaultConfig())

	result := service.EnsureAssignment("entry-1", "whatsapp", "phone-1")

	require.Empty(t, result)
	require.Nil(t, repo.assigned)
}

func TestEnsureAssignment_NoConnectedUsers_WithDepartment(t *testing.T) {
	eligible := &mockEligible{
		workspaceUsers:  []string{"ws-user"},
		departmentUsers: map[string][]string{},
	}
	repo := &mockRepo{}
	service := newService(repo, eligible, defaultResolver("ws-1", "dept-1"), defaultConfig())

	result := service.EnsureAssignment("entry-1", "whatsapp", "phone-1")

	require.Empty(t, result)
	require.Nil(t, repo.assigned)
}

func TestEnsureAssignment_GetRoundRobinStateError(t *testing.T) {
	repo := &mockRepo{roundRobinErr: errors.New("state query failed")}
	service := newService(repo, defaultEligible("u1"), defaultResolver("ws-1", ""), defaultConfig())

	result := service.EnsureAssignment("entry-1", "whatsapp", "phone-1")

	require.Empty(t, result)
	require.Nil(t, repo.assigned)
}

func TestEnsureAssignment_FirstAssignment_NilState(t *testing.T) {
	repo := &mockRepo{}
	eligible := defaultEligible("charlie", "alice", "bob")
	service := newService(repo, eligible, defaultResolver("ws-1", ""), defaultConfig())

	result := service.EnsureAssignment("entry-1", "whatsapp", "phone-1")

	require.Equal(t, "alice", result)
	require.Equal(t, "alice", repo.roundRobin.LastAssignedUserID)
}

func TestEnsureAssignment_SubsequentAssignment_Wraps(t *testing.T) {
	repo := &mockRepo{
		roundRobin: &ia.RoundRobinState{
			ID:                 "state-1",
			WorkspaceID:        "ws-1",
			BusinessPhoneID:    "phone-1",
			LastAssignedUserID: "charlie",
		},
	}

	eligible := defaultEligible("charlie", "alice", "bob")
	service := newService(repo, eligible, defaultResolver("ws-1", ""), defaultConfig())

	result := service.EnsureAssignment("entry-1", "whatsapp", "phone-1")

	require.Equal(t, "alice", result)
	require.Equal(t, "alice", repo.roundRobin.LastAssignedUserID)
}

func TestEnsureAssignment_AssignError(t *testing.T) {
	repo := &mockRepo{assignErr: errors.New("db write failed")}
	service := newService(repo, defaultEligible("u1"), defaultResolver("ws-1", ""), defaultConfig())

	result := service.EnsureAssignment("entry-1", "whatsapp", "phone-1")

	require.Empty(t, result)
}

func TestEnsureAssignment_SaveStateError_NonFatal(t *testing.T) {
	repo := &mockRepo{saveStateErr: errors.New("state write failed")}
	service := newService(repo, defaultEligible("u1"), defaultResolver("ws-1", ""), defaultConfig())

	result := service.EnsureAssignment("entry-1", "whatsapp", "phone-1")

	require.Equal(t, "u1", result)
	require.NotNil(t, repo.assigned)
}

func TestEnsureAssignment_StatePreservesExistingID(t *testing.T) {
	repo := &mockRepo{
		roundRobin: &ia.RoundRobinState{
			ID:                 "existing-state-id",
			WorkspaceID:        "ws-1",
			BusinessPhoneID:    "phone-1",
			LastAssignedUserID: "u1",
		},
	}
	service := newService(repo, defaultEligible("u1", "u2"), defaultResolver("ws-1", ""), defaultConfig())

	service.EnsureAssignment("entry-1", "whatsapp", "phone-1")

	require.NotEmpty(t, repo.savedStates)
	require.Equal(t, "existing-state-id", repo.savedStates[0].ID)
}

func TestEnsureAssignment_SortOrderDeterministic(t *testing.T) {
	repo := &mockRepo{}

	eligible := defaultEligible("zara", "mike", "alice")
	service := newService(repo, eligible, defaultResolver("ws-1", ""), defaultConfig())

	result := service.EnsureAssignment("entry-1", "whatsapp", "phone-1")

	require.Equal(t, "alice", result)
}

func TestEnsureAssignment_AssignmentFieldsCorrect(t *testing.T) {
	repo := &mockRepo{}
	service := newService(repo, defaultEligible("u1"), defaultResolver("ws-1", ""), defaultConfig())

	service.EnsureAssignment("entry-42", "whatsapp", "phone-99")

	require.NotNil(t, repo.assigned)
	assert.Equal(t, "ws-1", repo.assigned.WorkspaceID)
	assert.Equal(t, "phone-99", repo.assigned.BusinessPhoneID)
	assert.Equal(t, "entry-42", repo.assigned.EntryID)
	assert.Equal(t, "whatsapp", repo.assigned.EntryType)
	assert.Equal(t, "u1", repo.assigned.AssignedUserID)
}

func TestEnsureAssignment_RoundRobinStateFieldsCorrect(t *testing.T) {
	repo := &mockRepo{}
	service := newService(repo, defaultEligible("u1"), defaultResolver("ws-1", ""), defaultConfig())

	service.EnsureAssignment("entry-1", "whatsapp", "phone-99")

	require.NotEmpty(t, repo.savedStates)
	state := repo.savedStates[0]
	assert.Equal(t, "ws-1", state.WorkspaceID)
	assert.Equal(t, "phone-99", state.BusinessPhoneID)
	assert.Equal(t, "u1", state.LastAssignedUserID)
}

func TestEnsureAssignment_SingleUser_GetsAll(t *testing.T) {
	repo := newStatefulRepo()
	eligible := defaultEligible("only-user")
	resolver := defaultResolver("ws-1", "")
	service := newService(repo, eligible, resolver, defaultConfig())

	for i := 0; i < 10; i++ {
		result := service.EnsureAssignment(fmt.Sprintf("entry-%d", i), "whatsapp", "phone-1")
		require.Equal(t, "only-user", result)
	}
}

func TestEnsureAssignment_EvenDistribution_NoDepartment(t *testing.T) {
	repo := newStatefulRepo()
	users := []string{"alice", "bob", "charlie"}
	eligible := defaultEligible(users...)
	resolver := defaultResolver("ws-1", "")
	service := newService(repo, eligible, resolver, defaultConfig())

	counts := map[string]int{}
	n := 120
	for i := 0; i < n; i++ {
		id := service.EnsureAssignment(fmt.Sprintf("entry-%d", i), "whatsapp", "phone-1")
		counts[id]++
	}

	for _, u := range []string{"alice", "bob", "charlie"} {
		assert.Equal(t, n/3, counts[u], "user %s should get exactly %d assignments", u, n/3)
	}
}

func TestEnsureAssignment_EvenDistribution_WithDepartment(t *testing.T) {
	repo := newStatefulRepo()
	eligible := deptEligible(
		[]string{"ws-user"},
		"ws-1:dept-1",
		[]string{"dept-alice", "dept-bob", "dept-charlie"},
	)
	resolver := defaultResolver("ws-1", "dept-1")
	service := newService(repo, eligible, resolver, defaultConfig())

	counts := map[string]int{}
	n := 90
	for i := 0; i < n; i++ {
		id := service.EnsureAssignment(fmt.Sprintf("entry-%d", i), "whatsapp", "phone-1")
		counts[id]++
	}

	for _, u := range []string{"dept-alice", "dept-bob", "dept-charlie"} {
		assert.Equal(t, 30, counts[u], "user %s should get exactly 30 assignments", u)
	}
}

func TestEnsureAssignment_EvenDistribution_LargeScale(t *testing.T) {
	repo := newStatefulRepo()
	users := []string{"user-a", "user-b", "user-c", "user-d", "user-e"}
	eligible := defaultEligible(users...)
	resolver := defaultResolver("ws-1", "")
	service := newService(repo, eligible, resolver, defaultConfig())

	counts := map[string]int{}
	n := 500
	for i := 0; i < n; i++ {
		id := service.EnsureAssignment(fmt.Sprintf("entry-%d", i), "whatsapp", "phone-1")
		counts[id]++
	}

	for _, u := range users {
		assert.Equal(t, 100, counts[u], "user %s should get exactly 100 assignments", u)
	}
}

func TestEnsureAssignment_DynamicPool_SkewsDistribution(t *testing.T) {

	repo := newStatefulRepo()
	resolver := defaultResolver("ws-1", "")
	cfg := defaultConfig()

	eligible := defaultEligible("alice", "bob", "charlie")
	svc := newService(repo, eligible, resolver, cfg)

	counts := map[string]int{}
	for i := 0; i < 3; i++ {
		id := svc.EnsureAssignment(fmt.Sprintf("entry-%d", i), "whatsapp", "phone-1")
		counts[id]++
	}

	eligible2 := defaultEligible("alice", "charlie")
	svc2 := newService(repo, eligible2, resolver, cfg)

	for i := 3; i < 6; i++ {
		id := svc2.EnsureAssignment(fmt.Sprintf("entry-%d", i), "whatsapp", "phone-1")
		counts[id]++
	}

	eligible3 := defaultEligible("alice", "bob", "charlie")
	svc3 := newService(repo, eligible3, resolver, cfg)

	for i := 6; i < 9; i++ {
		id := svc3.EnsureAssignment(fmt.Sprintf("entry-%d", i), "whatsapp", "phone-1")
		counts[id]++
	}

	t.Logf("Distribution with dynamic pool changes: %v", counts)

	totalAssignments := 0
	for _, c := range counts {
		totalAssignments += c
	}
	require.Equal(t, 9, totalAssignments, "all 9 entries should be assigned")

	isUneven := false
	for _, c := range counts {
		if c != 3 {
			isUneven = true
			break
		}
	}

	if isUneven {
		t.Logf("BUG CONFIRMED: dynamic user pool causes uneven distribution: %v", counts)
	} else {
		t.Logf("Distribution happened to be even — try different pool change patterns")
	}
}

func TestEnsureAssignment_NearestSuccessor_LastAssignedGoesOffline(t *testing.T) {
	repo := newStatefulRepo()
	resolver := defaultResolver("ws-1", "")
	cfg := defaultConfig()

	eligible1 := defaultEligible("alice", "bob", "charlie", "dave")
	svc1 := newService(repo, eligible1, resolver, cfg)
	for i := 0; i < 4; i++ {
		svc1.EnsureAssignment(fmt.Sprintf("entry-%d", i), "whatsapp", "phone-1")
	}

	repo2 := newStatefulRepo()
	eligible2 := defaultEligible("alice", "bob", "charlie", "dave")
	svc2 := newService(repo2, eligible2, resolver, cfg)
	svc2.EnsureAssignment("e-0", "whatsapp", "phone-1")
	svc2.EnsureAssignment("e-1", "whatsapp", "phone-1")

	eligible3 := defaultEligible("alice", "charlie", "dave")
	svc3 := newService(repo2, eligible3, resolver, cfg)

	r := svc3.EnsureAssignment("e-2", "whatsapp", "phone-1")
	require.Equal(t, "charlie", r, "should continue to charlie (bob's successor), not restart at alice")

	r2 := svc3.EnsureAssignment("e-3", "whatsapp", "phone-1")
	require.Equal(t, "dave", r2, "should continue to dave after charlie")

	r3 := svc3.EnsureAssignment("e-4", "whatsapp", "phone-1")
	require.Equal(t, "alice", r3, "should wrap around to alice")
}

func TestEnsureAssignment_MultiplePhones_IndependentRR(t *testing.T) {
	repo := newStatefulRepo()
	eligible := defaultEligible("alice", "bob")
	resolver := defaultResolver("ws-1", "")
	service := newService(repo, eligible, resolver, defaultConfig())

	r1 := service.EnsureAssignment("entry-p1-0", "whatsapp", "phone-1")
	require.Equal(t, "alice", r1)

	r2 := service.EnsureAssignment("entry-p2-0", "whatsapp", "phone-2")
	require.Equal(t, "alice", r2)

	r3 := service.EnsureAssignment("entry-p1-1", "whatsapp", "phone-1")
	require.Equal(t, "bob", r3)

	r4 := service.EnsureAssignment("entry-p2-1", "whatsapp", "phone-2")
	require.Equal(t, "bob", r4)
}

func TestEnsureAssignment_Idempotent(t *testing.T) {
	repo := newStatefulRepo()
	eligible := defaultEligible("alice", "bob")
	resolver := defaultResolver("ws-1", "")
	service := newService(repo, eligible, resolver, defaultConfig())

	first := service.EnsureAssignment("entry-1", "whatsapp", "phone-1")
	require.Equal(t, "alice", first)

	second := service.EnsureAssignment("entry-1", "whatsapp", "phone-1")
	require.Equal(t, "alice", second)

	third := service.EnsureAssignment("entry-2", "whatsapp", "phone-1")
	require.Equal(t, "bob", third)
}

func TestEnsureAssignment_SkipAdmins_PassedToWorkspaceProvider(t *testing.T) {
	eligible := defaultEligible("user-a")
	cfg := &mockWorkspaceConfig{skipAdmins: true}
	service := newService(&mockRepo{}, eligible, defaultResolver("ws-1", ""), cfg)

	service.EnsureAssignment("entry-1", "whatsapp", "phone-1")

	require.Len(t, eligible.workspaceCalls, 1)
	assert.True(t, eligible.workspaceCalls[0].skipAdmins)
}

func TestEnsureAssignment_SkipAdmins_PassedToDepartmentProvider(t *testing.T) {
	eligible := deptEligible(nil, "ws-1:dept-1", []string{"dept-user"})
	cfg := &mockWorkspaceConfig{skipAdmins: true}
	service := newService(&mockRepo{}, eligible, defaultResolver("ws-1", "dept-1"), cfg)

	service.EnsureAssignment("entry-1", "whatsapp", "phone-1")

	require.Len(t, eligible.departmentCalls, 1)
	assert.True(t, eligible.departmentCalls[0].skipAdmins)
	assert.Equal(t, "ws-1", eligible.departmentCalls[0].workspaceID)
	assert.Equal(t, "dept-1", eligible.departmentCalls[0].departmentID)
}

func TestEnsureAssignment_FullCycleWrapAround(t *testing.T) {
	repo := newStatefulRepo()
	eligible := defaultEligible("alice", "bob", "charlie")
	resolver := defaultResolver("ws-1", "")
	service := newService(repo, eligible, resolver, defaultConfig())

	expected := []string{"alice", "bob", "charlie", "alice", "bob", "charlie"}
	for i, want := range expected {
		got := service.EnsureAssignment(fmt.Sprintf("entry-%d", i), "whatsapp", "phone-1")
		assert.Equal(t, want, got, "entry-%d should go to %s", i, want)
	}
}

func TestGetAssignedUserID_Found(t *testing.T) {
	repo := &mockRepo{
		findByEntryResult: &ia.InboxAssignment{AssignedUserID: "user-x"},
	}
	service := newService(repo, defaultEligible(), defaultResolver("ws-1", ""), defaultConfig())

	result := service.GetAssignedUserID("ws-1", "entry-1", "whatsapp")

	require.Equal(t, "user-x", result)
}

func TestGetAssignedUserID_NotFound(t *testing.T) {
	repo := &mockRepo{findByEntryResult: nil}
	service := newService(repo, defaultEligible(), defaultResolver("ws-1", ""), defaultConfig())

	result := service.GetAssignedUserID("ws-1", "entry-1", "whatsapp")

	require.Empty(t, result)
}

func TestGetAssignedUserID_Error(t *testing.T) {
	repo := &mockRepo{findByEntryErr: errors.New("db error")}
	service := newService(repo, defaultEligible(), defaultResolver("ws-1", ""), defaultConfig())

	result := service.GetAssignedUserID("ws-1", "entry-1", "whatsapp")

	require.Empty(t, result)
}

func TestIsAssignedToUser_True(t *testing.T) {
	repo := &mockRepo{isAssignedResult: true}
	service := newService(repo, defaultEligible(), defaultResolver("ws-1", ""), defaultConfig())

	result := service.IsAssignedToUser("ws-1", "entry-1", "whatsapp", "user-1")

	require.True(t, result)
}

func TestIsAssignedToUser_False(t *testing.T) {
	repo := &mockRepo{isAssignedResult: false}
	service := newService(repo, defaultEligible(), defaultResolver("ws-1", ""), defaultConfig())

	result := service.IsAssignedToUser("ws-1", "entry-1", "whatsapp", "user-1")

	require.False(t, result)
}

func TestIsAssignedToUser_Error(t *testing.T) {
	repo := &mockRepo{isAssignedErr: errors.New("db error")}
	service := newService(repo, defaultEligible(), defaultResolver("ws-1", ""), defaultConfig())

	result := service.IsAssignedToUser("ws-1", "entry-1", "whatsapp", "user-1")

	require.False(t, result)
}

func TestReassign_Success(t *testing.T) {
	repo := &mockRepo{}
	service := newService(repo, defaultEligible(), defaultResolver("ws-1", ""), defaultConfig())

	err := service.Reassign("entry-1", "whatsapp", "phone-1", "ws-1", "target-user")

	require.NoError(t, err)
	require.NotNil(t, repo.assigned)
	assert.Equal(t, "entry-1", repo.assigned.EntryID)
	assert.Equal(t, "whatsapp", repo.assigned.EntryType)
	assert.Equal(t, "phone-1", repo.assigned.BusinessPhoneID)
	assert.Equal(t, "ws-1", repo.assigned.WorkspaceID)
	assert.Equal(t, "target-user", repo.assigned.AssignedUserID)
}

func TestReassign_Error(t *testing.T) {
	repo := &mockRepo{assignErr: errors.New("db write failed")}
	service := newService(repo, defaultEligible(), defaultResolver("ws-1", ""), defaultConfig())

	err := service.Reassign("entry-1", "whatsapp", "phone-1", "ws-1", "target-user")

	require.Error(t, err)
}

func TestEnsureAssignment_ProductionScenario_110Entries(t *testing.T) {

	repo := newStatefulRepo()
	deptUsers := []string{"felipe", "ana", "carlos", "maria", "joao"}
	eligible := deptEligible(nil, "ws-prod:dept-vendas", deptUsers)
	resolver := defaultResolver("ws-prod", "dept-vendas")
	service := newService(repo, eligible, resolver, defaultConfig())

	counts := map[string]int{}
	n := 110
	for i := 0; i < n; i++ {
		id := service.EnsureAssignment(fmt.Sprintf("entry-%d", i), "whatsapp", "phone-1")
		require.NotEmpty(t, id, "entry-%d should be assigned", i)
		counts[id]++
	}

	t.Logf("Stable-pool distribution for %d entries across %d users: %v", n, len(deptUsers), counts)

	for _, u := range deptUsers {
		assert.Equal(t, 22, counts[u], "user %s should get 22 assignments", u)
	}
}

func TestEnsureAssignment_ProductionScenario_OnlineOfflineFlicker(t *testing.T) {
	repo := newStatefulRepo()
	resolver := defaultResolver("ws-prod", "dept-vendas")
	cfg := defaultConfig()

	allUsers := []string{"ana", "carlos", "felipe", "joao", "maria"}

	counts := map[string]int{}
	entryNum := 0

	assign := func(users []string, n int) {
		eligible := deptEligible(nil, "ws-prod:dept-vendas", users)
		svc := newService(repo, eligible, resolver, cfg)
		for i := 0; i < n; i++ {
			id := svc.EnsureAssignment(fmt.Sprintf("entry-%d", entryNum), "whatsapp", "phone-1")
			if id != "" {
				counts[id]++
			}
			entryNum++
		}
	}

	assign(allUsers, 20)

	assign([]string{"ana", "carlos", "felipe", "maria"}, 15)

	assign([]string{"ana", "felipe", "joao", "maria"}, 20)

	assign(allUsers, 25)

	assign([]string{"carlos", "felipe", "joao"}, 15)

	assign(allUsers, 15)

	total := 20 + 15 + 20 + 25 + 15 + 15
	t.Logf("Dynamic pool over %d entries: %v", total, counts)

	totalAssigned := 0
	for _, c := range counts {
		totalAssigned += c
	}
	require.Equal(t, total, totalAssigned)

	maxCount := 0
	minCount := total
	for _, c := range counts {
		if c > maxCount {
			maxCount = c
		}
		if c < minCount {
			minCount = c
		}
	}
	skew := maxCount - minCount
	t.Logf("Skew: max=%d, min=%d, difference=%d (ideal per user: %d)", maxCount, minCount, skew, total/len(allUsers))

	if skew > 5 {
		t.Logf("BUG CONFIRMED: skew of %d across %d entries shows uneven distribution due to dynamic pool changes", skew, total)
	}
}

type multiDeptEligible struct {
	workspaceUsers  []string
	departmentUsers map[string][]string
}

func (e *multiDeptEligible) GetEligibleUsersForWorkspace(string, bool) []string {
	return append([]string(nil), e.workspaceUsers...)
}

func (e *multiDeptEligible) GetEligibleUsersForWorkspaceDepartment(wsID, deptID string, _ bool) []string {
	key := wsID + ":" + deptID
	return append([]string(nil), e.departmentUsers[key]...)
}

type multiDeptResolver struct {
	workspaceID string
	entryToDept map[string]string
}

func (r *multiDeptResolver) GetCampaignWorkspaceID(string, string) (string, error) {
	return r.workspaceID, nil
}
func (r *multiDeptResolver) GetCampaignDepartmentID(string, string) (string, error) {
	return "", nil
}
func (r *multiDeptResolver) GetEntryWorkspaceID(string, string) (string, error) {
	return r.workspaceID, nil
}
func (r *multiDeptResolver) GetEntryDepartmentID(entryID, _ string) (string, error) {
	return r.entryToDept[entryID], nil
}
func (r *multiDeptResolver) GetEntryCampaignID(string, string) (string, error) {
	return "campaign-1", nil
}

var _ conversation.CampaignWorkspaceResolver = (*multiDeptResolver)(nil)

func TestEnsureAssignment_MultipleDepts_SharedRRState_CausesSkew(t *testing.T) {
	repo := newStatefulRepo()

	eligible := &multiDeptEligible{
		departmentUsers: map[string][]string{

			"ws-1:dept-a": {"alice", "bob"},

			"ws-1:dept-b": {"charlie", "dave", "eve"},
		},
	}

	entryToDept := map[string]string{}

	entryNum := 0
	deptACounts := map[string]int{}
	deptBCounts := map[string]int{}

	makeEntry := func(dept string) string {
		id := fmt.Sprintf("entry-%d", entryNum)
		entryToDept[id] = dept
		entryNum++
		return id
	}

	resolver := &multiDeptResolver{workspaceID: "ws-1", entryToDept: entryToDept}
	service := newService(repo, eligible, resolver, defaultConfig())

	for i := 0; i < 12; i++ {
		if i%2 == 0 {
			entryID := makeEntry("dept-a")
			id := service.EnsureAssignment(entryID, "whatsapp", "phone-1")
			deptACounts[id]++
			t.Logf("dept-a: %s → %s", entryID, id)
		} else {
			entryID := makeEntry("dept-b")
			id := service.EnsureAssignment(entryID, "whatsapp", "phone-1")
			deptBCounts[id]++
			t.Logf("dept-b: %s → %s", entryID, id)
		}
	}

	t.Logf("Dept A distribution (2 users, 6 entries): %v", deptACounts)
	t.Logf("Dept B distribution (3 users, 6 entries): %v", deptBCounts)

	deptASkewed := deptACounts["alice"] != 3 || deptACounts["bob"] != 3
	if deptASkewed {
		t.Logf("BUG #2 CONFIRMED (dept-a): shared RR state caused skew: %v (expected alice=3, bob=3)", deptACounts)
	}
	deptBSkewed := deptBCounts["charlie"] != 2 || deptBCounts["dave"] != 2 || deptBCounts["eve"] != 2
	if deptBSkewed {
		t.Logf("BUG #2 CONFIRMED (dept-b): shared RR state caused skew: %v (expected charlie=2, dave=2, eve=2)", deptBCounts)
	}

	if !deptASkewed && !deptBSkewed {
		t.Log("Distributions happened to be even despite shared state — try different interleaving")
	}
}

func TestEnsureAssignment_DynamicPool_Department_SkewsDistribution(t *testing.T) {
	repo := newStatefulRepo()
	cfg := defaultConfig()

	counts := map[string]int{}
	entryNum := 0

	assign := func(deptUsers []string, n int) {
		eligible := deptEligible(nil, "ws-1:dept-1", deptUsers)
		resolver := defaultResolver("ws-1", "dept-1")
		svc := newService(repo, eligible, resolver, cfg)
		for i := 0; i < n; i++ {
			id := svc.EnsureAssignment(fmt.Sprintf("entry-%d", entryNum), "whatsapp", "phone-1")
			if id != "" {
				counts[id]++
			}
			entryNum++
		}
	}

	assign([]string{"alice", "bob", "charlie"}, 9)

	assign([]string{"alice", "charlie"}, 6)

	assign([]string{"alice", "bob", "charlie"}, 9)

	total := 24
	t.Logf("Department dynamic pool distribution: %v (total: %d)", counts, total)

	totalAssigned := 0
	for _, c := range counts {
		totalAssigned += c
	}
	require.Equal(t, total, totalAssigned)

	maxCount, minCount := 0, total
	for _, c := range counts {
		if c > maxCount {
			maxCount = c
		}
		if c < minCount {
			minCount = c
		}
	}
	skew := maxCount - minCount
	t.Logf("Dept skew: max=%d, min=%d, diff=%d", maxCount, minCount, skew)

	require.True(t, counts["bob"] < counts["alice"] || counts["bob"] < counts["charlie"],
		"bob should get fewer since he was offline, but index bug makes it worse")
}

func TestEnsureAssignment_SingleDepartment_StablePool_EvenDistribution(t *testing.T) {
	repo := newStatefulRepo()
	eligible := deptEligible(nil, "ws-1:dept-only", []string{"user-a", "user-b", "user-c", "user-d"})
	resolver := defaultResolver("ws-1", "dept-only")
	service := newService(repo, eligible, resolver, defaultConfig())

	counts := map[string]int{}
	n := 100
	for i := 0; i < n; i++ {
		id := service.EnsureAssignment(fmt.Sprintf("entry-%d", i), "whatsapp", "phone-1")
		require.NotEmpty(t, id)
		counts[id]++
	}

	for _, u := range []string{"user-a", "user-b", "user-c", "user-d"} {
		assert.Equal(t, 25, counts[u], "user %s should get exactly 25", u)
	}
}

func TestEnsureAssignment_SingleDepartmentMember_GetsAll(t *testing.T) {
	repo := newStatefulRepo()
	eligible := deptEligible(nil, "ws-1:dept-solo", []string{"only-member"})
	resolver := defaultResolver("ws-1", "dept-solo")
	service := newService(repo, eligible, resolver, defaultConfig())

	for i := 0; i < 20; i++ {
		id := service.EnsureAssignment(fmt.Sprintf("entry-%d", i), "whatsapp", "phone-1")
		require.Equal(t, "only-member", id)
	}
}

func TestEnsureAssignment_WorkspaceOnly_TwoUsers_Alternates(t *testing.T) {
	repo := newStatefulRepo()
	eligible := defaultEligible("alpha", "bravo")
	resolver := defaultResolver("ws-1", "")
	service := newService(repo, eligible, resolver, defaultConfig())

	results := make([]string, 6)
	for i := 0; i < 6; i++ {
		results[i] = service.EnsureAssignment(fmt.Sprintf("entry-%d", i), "whatsapp", "phone-1")
	}

	require.Equal(t, []string{"alpha", "bravo", "alpha", "bravo", "alpha", "bravo"}, results)
}

func TestEnsureAssignment_WorkspaceOnly_DynamicPool_UserDrop(t *testing.T) {
	repo := newStatefulRepo()
	cfg := defaultConfig()

	eligible1 := defaultEligible("alice", "bob", "charlie")
	svc1 := newService(repo, eligible1, defaultResolver("ws-1", ""), cfg)

	r0 := svc1.EnsureAssignment("entry-0", "whatsapp", "phone-1")
	r1 := svc1.EnsureAssignment("entry-1", "whatsapp", "phone-1")
	require.Equal(t, "alice", r0)
	require.Equal(t, "bob", r1)

	eligible2 := defaultEligible("alice", "bob")
	svc2 := newService(repo, eligible2, defaultResolver("ws-1", ""), cfg)

	r2 := svc2.EnsureAssignment("entry-2", "whatsapp", "phone-1")

	require.Equal(t, "alice", r2)
	t.Logf("After charlie dropped: alice gets consecutive (idx wrapped to 0)")
}

func TestEnsureAssignment_WorkspaceOnly_DynamicPool_UserJoin(t *testing.T) {
	repo := newStatefulRepo()
	cfg := defaultConfig()

	eligible1 := defaultEligible("alice", "bob")
	svc1 := newService(repo, eligible1, defaultResolver("ws-1", ""), cfg)

	r0 := svc1.EnsureAssignment("entry-0", "whatsapp", "phone-1")
	r1 := svc1.EnsureAssignment("entry-1", "whatsapp", "phone-1")
	require.Equal(t, "alice", r0)
	require.Equal(t, "bob", r1)

	eligible2 := defaultEligible("alice", "bob", "charlie")
	svc2 := newService(repo, eligible2, defaultResolver("ws-1", ""), cfg)

	r2 := svc2.EnsureAssignment("entry-2", "whatsapp", "phone-1")

	require.Equal(t, "charlie", r2)

	r3 := svc2.EnsureAssignment("entry-3", "whatsapp", "phone-1")

	require.Equal(t, "alice", r3)
	t.Logf("After charlie joined: alice and charlie get extras, bob gets skipped once")
}

func TestEnsureAssignment_DepartmentAllOffline_NoStateChange(t *testing.T) {
	repo := newStatefulRepo()
	eligible := deptEligible([]string{"ws-user"}, "ws-1:dept-1", []string{})
	resolver := defaultResolver("ws-1", "dept-1")
	service := newService(repo, eligible, resolver, defaultConfig())

	result := service.EnsureAssignment("entry-1", "whatsapp", "phone-1")

	require.Empty(t, result)

	_, hasState := repo.rrStates[rrKey("ws-1", "phone-1", "dept-1")]
	require.False(t, hasState, "no RR state should be saved when no users available")
}

func TestEnsureAssignment_SkipAdmins_DepartmentScope(t *testing.T) {
	eligible := &mockEligible{
		workspaceUsers:  []string{"admin-user"},
		departmentUsers: map[string][]string{"ws-1:dept-1": {"regular-user"}},
	}
	cfg := &mockWorkspaceConfig{skipAdmins: true}
	repo := &mockRepo{}
	service := newService(repo, eligible, defaultResolver("ws-1", "dept-1"), cfg)

	result := service.EnsureAssignment("entry-1", "whatsapp", "phone-1")

	require.Equal(t, "regular-user", result)
	require.Len(t, eligible.departmentCalls, 1)
	require.True(t, eligible.departmentCalls[0].skipAdmins)
}

func TestEnsureAssignment_Idempotent_Department(t *testing.T) {
	repo := newStatefulRepo()
	eligible := deptEligible(nil, "ws-1:dept-1", []string{"alice", "bob"})
	resolver := defaultResolver("ws-1", "dept-1")
	service := newService(repo, eligible, resolver, defaultConfig())

	first := service.EnsureAssignment("entry-1", "whatsapp", "phone-1")
	require.Equal(t, "alice", first)

	second := service.EnsureAssignment("entry-1", "whatsapp", "phone-1")
	require.Equal(t, "alice", second, "same entry returns same user")

	third := service.EnsureAssignment("entry-2", "whatsapp", "phone-1")
	require.Equal(t, "bob", third, "RR did not advance for duplicate call")
}

func TestEnsureAssignment_ProductionScenario_MultipleDepts_HighVolume(t *testing.T) {
	repo := newStatefulRepo()

	eligible := &multiDeptEligible{
		departmentUsers: map[string][]string{
			"ws-1:vendas":  {"felipe", "ana", "carlos"},
			"ws-1:suporte": {"maria", "joao"},
			"ws-1:admin":   {"pedro"},
		},
	}

	entryToDept := map[string]string{}
	resolver := &multiDeptResolver{workspaceID: "ws-1", entryToDept: entryToDept}
	service := newService(repo, eligible, resolver, defaultConfig())

	vendasCounts := map[string]int{}
	suporteCounts := map[string]int{}
	adminCounts := map[string]int{}

	entryNum := 0
	assignN := func(dept string, n int, counts map[string]int) {
		for i := 0; i < n; i++ {
			id := fmt.Sprintf("entry-%d", entryNum)
			entryToDept[id] = dept
			result := service.EnsureAssignment(id, "whatsapp", "phone-1")
			require.NotEmpty(t, result)
			counts[result]++
			entryNum++
		}
	}

	assignN("vendas", 10, vendasCounts)
	assignN("suporte", 5, suporteCounts)
	assignN("vendas", 10, vendasCounts)
	assignN("admin", 3, adminCounts)
	assignN("suporte", 5, suporteCounts)
	assignN("vendas", 10, vendasCounts)

	t.Logf("Vendas (3 users, 30 entries): %v", vendasCounts)
	t.Logf("Suporte (2 users, 10 entries): %v", suporteCounts)
	t.Logf("Admin (1 user, 3 entries): %v", adminCounts)

	vendasExpected := 10
	vendasSkewed := false
	for _, u := range []string{"felipe", "ana", "carlos"} {
		if vendasCounts[u] != vendasExpected {
			vendasSkewed = true
		}
	}
	suporteExpected := 5
	suporteSkewed := false
	for _, u := range []string{"maria", "joao"} {
		if suporteCounts[u] != suporteExpected {
			suporteSkewed = true
		}
	}

	if vendasSkewed {
		t.Logf("BUG: vendas distribution skewed: %v (expected each=%d)", vendasCounts, vendasExpected)
	}
	if suporteSkewed {
		t.Logf("BUG: suporte distribution skewed: %v (expected each=%d)", suporteCounts, suporteExpected)
	}

	require.Equal(t, 3, adminCounts["pedro"])
}

func TestEnsureAssignment_DifferentEntryTypes(t *testing.T) {
	repo := newStatefulRepo()
	eligible := defaultEligible("alice", "bob")
	resolver := defaultResolver("ws-1", "")
	service := newService(repo, eligible, resolver, defaultConfig())

	r1 := service.EnsureAssignment("entry-1", "whatsapp", "phone-1")
	require.Equal(t, "alice", r1)

	r2 := service.EnsureAssignment("entry-2", "sms", "phone-1")
	require.Equal(t, "bob", r2)

	r3 := service.EnsureAssignment("entry-1", "sms", "phone-1")
	require.Equal(t, "alice", r3)
}

func TestEnsureAssignment_HighIndex_SingleUser_Wraps(t *testing.T) {
	repo := &mockRepo{
		roundRobin: &ia.RoundRobinState{
			ID:                 "state-1",
			WorkspaceID:        "ws-1",
			BusinessPhoneID:    "phone-1",
			LastAssignedUserID: "gone-user",
		},
	}
	service := newService(repo, defaultEligible("only-user"), defaultResolver("ws-1", ""), defaultConfig())

	result := service.EnsureAssignment("entry-1", "whatsapp", "phone-1")

	require.Equal(t, "only-user", result)
	require.Equal(t, "only-user", repo.roundRobin.LastAssignedUserID)
}

func TestEnsureAssignment_MultiplePhones_EachPhoneDistributesEvenly(t *testing.T) {
	repo := newStatefulRepo()
	eligible := defaultEligible("alice", "bob", "charlie")
	resolver := defaultResolver("ws-1", "")
	service := newService(repo, eligible, resolver, defaultConfig())

	phone1Counts := map[string]int{}
	phone2Counts := map[string]int{}

	for i := 0; i < 30; i++ {
		id := service.EnsureAssignment(fmt.Sprintf("p1-entry-%d", i), "whatsapp", "phone-1")
		phone1Counts[id]++
	}
	for i := 0; i < 30; i++ {
		id := service.EnsureAssignment(fmt.Sprintf("p2-entry-%d", i), "whatsapp", "phone-2")
		phone2Counts[id]++
	}

	for _, u := range []string{"alice", "bob", "charlie"} {
		assert.Equal(t, 10, phone1Counts[u], "phone-1: user %s should get 10", u)
		assert.Equal(t, 10, phone2Counts[u], "phone-2: user %s should get 10", u)
	}
}

func TestEnsureAssignment_MultiplePhones_InterleavedAssignments(t *testing.T) {
	repo := newStatefulRepo()
	eligible := defaultEligible("alice", "bob")
	resolver := defaultResolver("ws-1", "")
	service := newService(repo, eligible, resolver, defaultConfig())

	var p1Results, p2Results []string
	for i := 0; i < 6; i++ {
		r1 := service.EnsureAssignment(fmt.Sprintf("p1-%d", i), "whatsapp", "phone-1")
		p1Results = append(p1Results, r1)
		r2 := service.EnsureAssignment(fmt.Sprintf("p2-%d", i), "whatsapp", "phone-2")
		p2Results = append(p2Results, r2)
	}

	expected := []string{"alice", "bob", "alice", "bob", "alice", "bob"}
	require.Equal(t, expected, p1Results, "phone-1 should alternate perfectly")
	require.Equal(t, expected, p2Results, "phone-2 should alternate perfectly")
}

func TestEnsureAssignment_MultiplePhones_WithDepartment(t *testing.T) {
	repo := newStatefulRepo()
	eligible := deptEligible(nil, "ws-1:dept-1", []string{"alice", "bob", "charlie"})
	resolver := defaultResolver("ws-1", "dept-1")
	service := newService(repo, eligible, resolver, defaultConfig())

	p1Counts := map[string]int{}
	p2Counts := map[string]int{}

	for i := 0; i < 18; i++ {
		id := service.EnsureAssignment(fmt.Sprintf("p1-%d", i), "whatsapp", "phone-1")
		p1Counts[id]++
	}
	for i := 0; i < 18; i++ {
		id := service.EnsureAssignment(fmt.Sprintf("p2-%d", i), "whatsapp", "phone-2")
		p2Counts[id]++
	}

	for _, u := range []string{"alice", "bob", "charlie"} {
		assert.Equal(t, 6, p1Counts[u], "phone-1 dept: %s should get 6", u)
		assert.Equal(t, 6, p2Counts[u], "phone-2 dept: %s should get 6", u)
	}
}

func TestEnsureAssignment_ThreePhones_DistributionConsistency(t *testing.T) {
	repo := newStatefulRepo()
	eligible := defaultEligible("u1", "u2", "u3", "u4")
	resolver := defaultResolver("ws-1", "")
	service := newService(repo, eligible, resolver, defaultConfig())

	phones := []string{"phone-a", "phone-b", "phone-c"}
	phoneCounts := map[string]map[string]int{}

	for _, p := range phones {
		phoneCounts[p] = map[string]int{}
		for i := 0; i < 20; i++ {
			id := service.EnsureAssignment(fmt.Sprintf("%s-entry-%d", p, i), "whatsapp", p)
			phoneCounts[p][id]++
		}
	}

	for _, p := range phones {
		for _, u := range []string{"u1", "u2", "u3", "u4"} {
			assert.Equal(t, 5, phoneCounts[p][u], "%s: %s should get 5", p, u)
		}
	}
}

func TestEnsureAssignment_UserInTwoDepartments_GetsDoubleAssignments(t *testing.T) {
	repo := newStatefulRepo()

	eligible := &multiDeptEligible{
		departmentUsers: map[string][]string{
			"ws-1:sales":   {"alice", "shared-user"},
			"ws-1:support": {"bob", "shared-user"},
		},
	}

	entryToDept := map[string]string{}
	resolver := &multiDeptResolver{workspaceID: "ws-1", entryToDept: entryToDept}
	service := newService(repo, eligible, resolver, defaultConfig())

	counts := map[string]int{}
	entryNum := 0

	assignN := func(dept string, n int) {
		for i := 0; i < n; i++ {
			id := fmt.Sprintf("entry-%d", entryNum)
			entryToDept[id] = dept
			result := service.EnsureAssignment(id, "whatsapp", "phone-1")
			require.NotEmpty(t, result)
			counts[result]++
			entryNum++
		}
	}

	assignN("sales", 10)
	assignN("support", 10)

	t.Logf("User in 2 depts — distribution: %v", counts)

	totalShared := counts["shared-user"]
	t.Logf("shared-user got %d out of 20 total (alice=%d, bob=%d)", totalShared, counts["alice"], counts["bob"])

	require.Greater(t, totalShared, 0, "shared-user should get some assignments")
}

func TestEnsureAssignment_UserInTwoDepts_SeparatePhones_ShowsDoubleLoad(t *testing.T) {
	repo := newStatefulRepo()

	eligible := &multiDeptEligible{
		departmentUsers: map[string][]string{
			"ws-1:sales":   {"alice", "shared-user"},
			"ws-1:support": {"bob", "shared-user"},
		},
	}

	entryToDept := map[string]string{}
	resolver := &multiDeptResolver{workspaceID: "ws-1", entryToDept: entryToDept}

	salesService := newService(repo, eligible, resolver, defaultConfig())
	supportService := newService(repo, eligible, resolver, defaultConfig())

	salesCounts := map[string]int{}
	supportCounts := map[string]int{}
	entryNum := 0

	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("entry-%d", entryNum)
		entryToDept[id] = "sales"
		result := salesService.EnsureAssignment(id, "whatsapp", "phone-sales")
		salesCounts[result]++
		entryNum++
	}

	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("entry-%d", entryNum)
		entryToDept[id] = "support"
		result := supportService.EnsureAssignment(id, "whatsapp", "phone-support")
		supportCounts[result]++
		entryNum++
	}

	t.Logf("Sales (phone-sales): %v", salesCounts)
	t.Logf("Support (phone-support): %v", supportCounts)

	assert.Equal(t, 10, salesCounts["alice"], "alice gets exactly half of sales")
	assert.Equal(t, 10, salesCounts["shared-user"], "shared-user gets exactly half of sales")

	assert.Equal(t, 10, supportCounts["bob"], "bob gets exactly half of support")
	assert.Equal(t, 10, supportCounts["shared-user"], "shared-user gets exactly half of support")

	totalShared := salesCounts["shared-user"] + supportCounts["shared-user"]
	t.Logf("DESIGN ISSUE: shared-user total=%d, alice=%d, bob=%d — shared-user gets 2x load",
		totalShared, salesCounts["alice"], supportCounts["bob"])
	assert.Equal(t, 20, totalShared, "shared-user gets double the load of single-dept users")
}

func TestEnsureAssignment_UserInTwoDepts_DynamicPool_GoesOffline(t *testing.T) {
	repo := newStatefulRepo()
	cfg := defaultConfig()

	entryToDept := map[string]string{}
	resolver := &multiDeptResolver{workspaceID: "ws-1", entryToDept: entryToDept}
	entryNum := 0

	eligible1 := &multiDeptEligible{
		departmentUsers: map[string][]string{
			"ws-1:sales":   {"alice", "shared-user"},
			"ws-1:support": {"bob", "shared-user"},
		},
	}
	svc1 := newService(repo, eligible1, resolver, cfg)

	salesCounts := map[string]int{}
	supportCounts := map[string]int{}

	for i := 0; i < 4; i++ {
		id := fmt.Sprintf("entry-%d", entryNum)
		entryToDept[id] = "sales"
		salesCounts[svc1.EnsureAssignment(id, "whatsapp", "phone-sales")]++
		entryNum++
	}
	for i := 0; i < 4; i++ {
		id := fmt.Sprintf("entry-%d", entryNum)
		entryToDept[id] = "support"
		supportCounts[svc1.EnsureAssignment(id, "whatsapp", "phone-support")]++
		entryNum++
	}

	eligible2 := &multiDeptEligible{
		departmentUsers: map[string][]string{
			"ws-1:sales":   {"alice"},
			"ws-1:support": {"bob"},
		},
	}
	svc2 := newService(repo, eligible2, resolver, cfg)

	for i := 0; i < 4; i++ {
		id := fmt.Sprintf("entry-%d", entryNum)
		entryToDept[id] = "sales"
		salesCounts[svc2.EnsureAssignment(id, "whatsapp", "phone-sales")]++
		entryNum++
	}
	for i := 0; i < 4; i++ {
		id := fmt.Sprintf("entry-%d", entryNum)
		entryToDept[id] = "support"
		supportCounts[svc2.EnsureAssignment(id, "whatsapp", "phone-support")]++
		entryNum++
	}

	t.Logf("Sales after shared-user offline: %v", salesCounts)
	t.Logf("Support after shared-user offline: %v", supportCounts)

	require.Equal(t, 6, salesCounts["alice"], "alice gets 2 + 4 remaining sales")
	require.Equal(t, 6, supportCounts["bob"], "bob gets 2 + 4 remaining support")
	require.Equal(t, 2, salesCounts["shared-user"], "shared only got 2 before going offline")
	require.Equal(t, 2, supportCounts["shared-user"])
}

func TestEnsureAssignment_UserInTwoDepts_OnlyMemberOfOneDept(t *testing.T) {
	repo := newStatefulRepo()

	eligible := &multiDeptEligible{
		departmentUsers: map[string][]string{
			"ws-1:small-dept": {"shared-user"},
			"ws-1:big-dept":   {"alice", "bob", "shared-user"},
		},
	}

	entryToDept := map[string]string{}
	resolver := &multiDeptResolver{workspaceID: "ws-1", entryToDept: entryToDept}
	service := newService(repo, eligible, resolver, defaultConfig())

	entryNum := 0
	smallCounts := map[string]int{}
	bigCounts := map[string]int{}

	for i := 0; i < 6; i++ {
		id := fmt.Sprintf("entry-%d", entryNum)
		entryToDept[id] = "small-dept"
		result := service.EnsureAssignment(id, "whatsapp", "phone-small")
		smallCounts[result]++
		entryNum++
	}

	for i := 0; i < 6; i++ {
		id := fmt.Sprintf("entry-%d", entryNum)
		entryToDept[id] = "big-dept"
		result := service.EnsureAssignment(id, "whatsapp", "phone-big")
		bigCounts[result]++
		entryNum++
	}

	require.Equal(t, 6, smallCounts["shared-user"], "sole member gets all small-dept entries")
	assert.Equal(t, 2, bigCounts["alice"])
	assert.Equal(t, 2, bigCounts["bob"])
	assert.Equal(t, 2, bigCounts["shared-user"])

	totalShared := smallCounts["shared-user"] + bigCounts["shared-user"]
	t.Logf("shared-user total: %d (small-dept=%d, big-dept=%d) — 4x alice's load",
		totalShared, smallCounts["shared-user"], bigCounts["shared-user"])
}

func TestEnsureAssignment_UserInTwoDepts_SharedState_SamePhone_MassiveSkew(t *testing.T) {
	repo := newStatefulRepo()

	eligible := &multiDeptEligible{
		departmentUsers: map[string][]string{
			"ws-1:sales":   {"alice", "shared-user"},
			"ws-1:support": {"bob", "charlie", "shared-user"},
		},
	}

	entryToDept := map[string]string{}
	resolver := &multiDeptResolver{workspaceID: "ws-1", entryToDept: entryToDept}
	service := newService(repo, eligible, resolver, defaultConfig())

	counts := map[string]int{}
	entryNum := 0

	assignN := func(dept string, n int) {
		for i := 0; i < n; i++ {
			id := fmt.Sprintf("entry-%d", entryNum)
			entryToDept[id] = dept
			result := service.EnsureAssignment(id, "whatsapp", "phone-1")
			if result != "" {
				counts[result]++
			}
			entryNum++
		}
	}

	assignN("sales", 5)
	assignN("support", 5)
	assignN("sales", 5)
	assignN("support", 5)

	t.Logf("User-in-2-depts + shared state + same phone: %v", counts)

	total := 0
	for _, c := range counts {
		total += c
	}
	require.Equal(t, 20, total, "all 20 entries assigned")

	maxC, minC := 0, total
	for _, c := range counts {
		if c > maxC {
			maxC = c
		}
		if c < minC {
			minC = c
		}
	}
	t.Logf("Skew: max=%d, min=%d, diff=%d", maxC, minC, maxC-minC)
}

type constrainedRepo struct {
	statefulRepo

	wsPhoneSeen map[string]bool
}

func newConstrainedRepo() *constrainedRepo {
	return &constrainedRepo{
		statefulRepo: statefulRepo{
			assignments: make(map[string]*ia.InboxAssignment),
			rrStates:    make(map[string]*ia.RoundRobinState),
		},
		wsPhoneSeen: make(map[string]bool),
	}
}

func (r *constrainedRepo) SaveRoundRobinState(state *ia.RoundRobinState) error {
	twoColKey := state.WorkspaceID + "|" + state.BusinessPhoneID
	threeColKey := rrKey(state.WorkspaceID, state.BusinessPhoneID, state.DepartmentID)

	_, threeColExists := r.rrStates[threeColKey]

	if !threeColExists && r.wsPhoneSeen[twoColKey] {

		return fmt.Errorf("ERROR: duplicate key value violates unique constraint \"idx_rr_state_ws_phone\" (SQLSTATE 23505)")
	}

	cp := *state
	r.rrStates[threeColKey] = &cp
	r.wsPhoneSeen[twoColKey] = true
	return nil
}

func TestEnsureAssignment_StaleIndex_DepartmentStateNeverSaved(t *testing.T) {
	repo := newConstrainedRepo()
	cfg := defaultConfig()

	deptA := "dept-aaa"
	wsID := "ws-1"
	phoneID := "phone-1"

	eligible := deptEligible(
		[]string{"alice", "bob", "charlie"},
		wsID+":"+deptA, []string{"alice", "bob", "charlie"},
	)

	resolver1 := defaultResolver(wsID, "")
	svc1 := newService(repo, eligible, resolver1, cfg)
	r1 := svc1.EnsureAssignment("entry-ws-0", "whatsapp", phoneID)
	require.NotEmpty(t, r1, "workspace-level assignment should succeed")

	state, _ := repo.GetRoundRobinState(wsID, phoneID, "")
	require.NotNil(t, state, "workspace-level RR state must exist")

	resolver2 := defaultResolver(wsID, deptA)
	svc2 := newService(repo, eligible, resolver2, cfg)

	counts := map[string]int{}
	for i := 0; i < 10; i++ {
		uid := svc2.EnsureAssignment(fmt.Sprintf("entry-dept-%d", i), "whatsapp", phoneID)
		require.NotEmpty(t, uid)
		counts[uid]++
	}

	t.Logf("Department distribution (stale-index bug): %v", counts)

	require.Len(t, counts, 1,
		"BUG: stale idx_rr_state_ws_phone causes all department entries to go to a single user; "+
			"expected 1 recipient but got %d — distribution: %v", len(counts), counts)

}

func TestEnsureAssignment_WithoutStaleIndex_DepartmentDistributes(t *testing.T) {
	repo := newStatefulRepo()
	cfg := defaultConfig()

	deptA := "dept-aaa"
	wsID := "ws-1"
	phoneID := "phone-1"

	eligible := deptEligible(
		[]string{"alice", "bob", "charlie"},
		wsID+":"+deptA, []string{"alice", "bob", "charlie"},
	)

	resolver1 := defaultResolver(wsID, "")
	svc1 := newService(repo, eligible, resolver1, cfg)
	svc1.EnsureAssignment("entry-ws-0", "whatsapp", phoneID)

	resolver2 := defaultResolver(wsID, deptA)
	svc2 := newService(repo, eligible, resolver2, cfg)

	counts := map[string]int{}
	for i := 0; i < 9; i++ {
		uid := svc2.EnsureAssignment(fmt.Sprintf("entry-dept-%d", i), "whatsapp", phoneID)
		require.NotEmpty(t, uid)
		counts[uid]++
	}

	t.Logf("Department distribution (no stale index): %v", counts)

	require.Len(t, counts, 3, "all 3 users should receive entries")
	for user, c := range counts {
		require.Equal(t, 3, c, "user %s should get exactly 3 entries", user)
	}
}

func TestEnsureAssignment_MultiplePhones_DynamicPool_PerPhoneSkew(t *testing.T) {
	repo := newStatefulRepo()
	cfg := defaultConfig()

	eligible1 := defaultEligible("alice", "bob", "charlie")
	svc1 := newService(repo, eligible1, defaultResolver("ws-1", ""), cfg)

	p1Counts := map[string]int{}
	p2Counts := map[string]int{}

	for i := 0; i < 6; i++ {
		p1Counts[svc1.EnsureAssignment(fmt.Sprintf("p1-%d", i), "whatsapp", "phone-1")]++
		p2Counts[svc1.EnsureAssignment(fmt.Sprintf("p2-%d", i), "whatsapp", "phone-2")]++
	}

	eligible2 := defaultEligible("alice", "charlie")
	svc2 := newService(repo, eligible2, defaultResolver("ws-1", ""), cfg)

	for i := 6; i < 12; i++ {
		p1Counts[svc2.EnsureAssignment(fmt.Sprintf("p1-%d", i), "whatsapp", "phone-1")]++
		p2Counts[svc2.EnsureAssignment(fmt.Sprintf("p2-%d", i), "whatsapp", "phone-2")]++
	}

	eligible3 := defaultEligible("alice", "bob", "charlie")
	svc3 := newService(repo, eligible3, defaultResolver("ws-1", ""), cfg)

	for i := 12; i < 18; i++ {
		p1Counts[svc3.EnsureAssignment(fmt.Sprintf("p1-%d", i), "whatsapp", "phone-1")]++
		p2Counts[svc3.EnsureAssignment(fmt.Sprintf("p2-%d", i), "whatsapp", "phone-2")]++
	}

	t.Logf("Phone-1 distribution (18 entries, 3 users): %v", p1Counts)
	t.Logf("Phone-2 distribution (18 entries, 3 users): %v", p2Counts)

	for u, c := range p1Counts {
		assert.Equal(t, p2Counts[u], c, "phone-1 and phone-2 should mirror: user %s", u)
	}

	require.Less(t, p1Counts["bob"], p1Counts["alice"], "bob should have fewer than alice")
}
