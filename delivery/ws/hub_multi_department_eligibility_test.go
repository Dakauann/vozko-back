package ws

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	workspace_department "vozko/domain/workspace/workspace_department"
)

// eligibilityFakeSharedState is a no-op SharedState whose SMembers returns a
// configurable set, so collectEligibleUsers can run without a real Redis.
type eligibilityFakeSharedState struct {
	members map[string][]string
}

func (s *eligibilityFakeSharedState) SetNX(string, string, time.Duration) (bool, error) {
	return true, nil
}
func (s *eligibilityFakeSharedState) SetString(string, string, time.Duration) error  { return nil }
func (s *eligibilityFakeSharedState) GetString(string) (string, error)               { return "", nil }
func (s *eligibilityFakeSharedState) Del(...string) error                            { return nil }
func (s *eligibilityFakeSharedState) Exists(string) (bool, error)                    { return false, nil }
func (s *eligibilityFakeSharedState) Incr(string) (int64, error)                     { return 0, nil }
func (s *eligibilityFakeSharedState) Decr(string) (int64, error)                     { return 0, nil }
func (s *eligibilityFakeSharedState) IncrWithTTL(string, time.Duration) (int64, error) {
	return 0, nil
}
func (s *eligibilityFakeSharedState) TryIncr(string, int64) (bool, error) { return true, nil }
func (s *eligibilityFakeSharedState) SAdd(string, ...string) error        { return nil }
func (s *eligibilityFakeSharedState) SRem(string, ...string) error        { return nil }
func (s *eligibilityFakeSharedState) SMembers(key string) ([]string, error) {
	if s.members == nil {
		return nil, nil
	}
	return s.members[key], nil
}
func (s *eligibilityFakeSharedState) Publish(string, []byte) error                  { return nil }
func (s *eligibilityFakeSharedState) Subscribe(context.Context, string, func([]byte)) {}
func (s *eligibilityFakeSharedState) HSet(string, string, string) error             { return nil }
func (s *eligibilityFakeSharedState) HDel(string, string) error                     { return nil }
func (s *eligibilityFakeSharedState) HGetAll(string) (map[string]string, error)     { return nil, nil }
func (s *eligibilityFakeSharedState) HIncrBy(string, string, int64) (int64, error)  { return 0, nil }
func (s *eligibilityFakeSharedState) IncrBy(string, int64) (int64, error)           { return 0, nil }
func (s *eligibilityFakeSharedState) DecrBy(string, int64) (int64, error)           { return 0, nil }
func (s *eligibilityFakeSharedState) TryIncrBy(string, int64, int64) (bool, error)  { return true, nil }
func (s *eligibilityFakeSharedState) Expire(string, time.Duration) (bool, error)    { return true, nil }

// noopWSMetricsRecorder is a do-nothing metrics.WSMetricsRecorder for tests that
// construct ws handlers/hubs but don't assert on metrics.
type noopWSMetricsRecorder struct{}

func (noopWSMetricsRecorder) IncWSConnections(string) {}
func (noopWSMetricsRecorder) DecWSConnections(string) {}

// fakeDepartmentMemberLister maps a departmentID to the user IDs of its members.
// A user listed under several departments models a multi-department member.
type fakeDepartmentMemberLister struct {
	membersByDept map[string][]string
}

func (l *fakeDepartmentMemberLister) ListMembers(departmentID string) ([]workspace_department.DepartmentMember, error) {
	userIDs := l.membersByDept[departmentID]
	members := make([]workspace_department.DepartmentMember, 0, len(userIDs))
	for _, uid := range userIDs {
		members = append(members, workspace_department.DepartmentMember{UserID: uid})
	}
	return members, nil
}

// TestRouletteEligibility_MultiDepartmentMember_IsEligibleInAllDepartments asserts the
// user's concern directly: an online operator who belongs to several departments must be
// part of the roulette pool for EVERY department they belong to — not just one.
func TestRouletteEligibility_MultiDepartmentMember_IsEligibleInAllDepartments(t *testing.T) {
	// bob ∈ {dept-a, dept-b}; alice ∈ {dept-a}; carol ∈ {dept-b}.
	deptRepo := &fakeDepartmentMemberLister{
		membersByDept: map[string][]string{
			"dept-a": {"alice", "bob"},
			"dept-b": {"bob", "carol"},
		},
	}

	authorizer := &hubDepartmentTestAuthorizer{} // roulette=true, not owner/admin, is member
	hub := NewConversationHub(authorizer, nil, &eligibilityFakeSharedState{}, "test-replica", "")
	hub.SetWorkspaceDepartmentRepo(deptRepo)

	// All three operators are simply online in ws-1.
	for _, uid := range []string{"alice", "bob", "carol"} {
		conn := &WSConnection{ID: uid, UserID: uid, WorkspaceID: "ws-1", Send: make(chan []byte, 1)}
		hub.connections[conn.ID] = conn
		hub.userConnections[uid] = map[string]bool{conn.ID: true}
	}

	deptA := hub.GetEligibleUsersForWorkspaceDepartment("ws-1", "dept-a", false)
	deptB := hub.GetEligibleUsersForWorkspaceDepartment("ws-1", "dept-b", false)

	require.ElementsMatch(t, []string{"alice", "bob"}, deptA,
		"dept-a roulette pool must be exactly its online members")
	require.ElementsMatch(t, []string{"bob", "carol"}, deptB,
		"dept-b roulette pool must include bob, who is a member of dept-b even though he also belongs to dept-a")
}

// TestRouletteEligibility_MultiDepartmentMember_CurrentViewDoesNotRestrict proves that the
// department the operator is *currently viewing* (conn.DepartmentID) never narrows the
// roulette pool — eligibility is membership-based, resolved from the conversation's department.
func TestRouletteEligibility_MultiDepartmentMember_CurrentViewDoesNotRestrict(t *testing.T) {
	deptRepo := &fakeDepartmentMemberLister{
		membersByDept: map[string][]string{
			"dept-a": {"bob"},
			"dept-b": {"bob"},
		},
	}

	authorizer := &hubDepartmentTestAuthorizer{}
	hub := NewConversationHub(authorizer, nil, &eligibilityFakeSharedState{}, "test-replica", "")
	hub.SetWorkspaceDepartmentRepo(deptRepo)

	// bob is currently *viewing* dept-a (conn.DepartmentID = "dept-a").
	conn := &WSConnection{ID: "bob", UserID: "bob", WorkspaceID: "ws-1", DepartmentID: "dept-a", Send: make(chan []byte, 1)}
	hub.connections[conn.ID] = conn
	hub.userConnections["bob"] = map[string]bool{conn.ID: true}

	// A conversation arrives in dept-b. bob must still be eligible despite viewing dept-a.
	deptB := hub.GetEligibleUsersForWorkspaceDepartment("ws-1", "dept-b", false)
	require.Contains(t, deptB, "bob",
		"bob's currently-selected department view must not exclude him from dept-b roulette")
}
