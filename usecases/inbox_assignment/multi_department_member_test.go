package inbox_assignment_usecase

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestEnsureAssignment_MemberInTwoDepartments_GetsAssignedFromBoth replicates the reported
// scenario: a single operator (bob) belongs to two departments. He must receive auto-assigned
// conversations from BOTH departments, not just one. The round-robin state is keyed per
// department, so bob's position in each department's rotation is independent.
func TestEnsureAssignment_MemberInTwoDepartments_GetsAssignedFromBoth(t *testing.T) {
	repo := newStatefulRepo()

	// bob is in dept-a (alongside alice) and dept-b (alongside carol).
	eligible := &multiDeptEligible{
		departmentUsers: map[string][]string{
			"ws-1:dept-a": {"alice", "bob"},
			"ws-1:dept-b": {"bob", "carol"},
		},
	}

	entryToDept := map[string]string{}
	resolver := &multiDeptResolver{workspaceID: "ws-1", entryToDept: entryToDept}
	service := newService(repo, eligible, resolver, defaultConfig())

	counts := map[string]int{}
	bobFromDeptA, bobFromDeptB := 0, 0

	// Interleave conversations across the two departments.
	for i := 0; i < 12; i++ {
		dept := "dept-a"
		if i%2 == 1 {
			dept = "dept-b"
		}
		entryID := fmt.Sprintf("entry-%d", i)
		entryToDept[entryID] = dept

		assigned := service.EnsureAssignment(entryID, "whatsapp", "phone-1")
		require.NotEmpty(t, assigned, "entry %s (%s) should be auto-assigned", entryID, dept)
		counts[assigned]++
		if assigned == "bob" {
			if dept == "dept-a" {
				bobFromDeptA++
			} else {
				bobFromDeptB++
			}
		}
	}

	t.Logf("distribution: %v (bob dept-a=%d, dept-b=%d)", counts, bobFromDeptA, bobFromDeptB)

	require.Greater(t, bobFromDeptA, 0,
		"bob is a dept-a member and must receive dept-a conversations")
	require.Greater(t, bobFromDeptB, 0,
		"bob is also a dept-b member and must ALSO receive dept-b conversations")

	// Sanity: every conversation went to a member of the right department.
	require.Equal(t, 12, counts["alice"]+counts["bob"]+counts["carol"],
		"all conversations must be assigned to eligible department members")
}
