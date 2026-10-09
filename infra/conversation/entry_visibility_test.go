package conversation

import (
	"errors"
	"fmt"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/inbox_assignment"
	"vozko/domain/shared"
	"vozko/domain/workspace"
)

type stubPlacements struct {
	placements map[string]conversation.EntryPlacement
	err        error
	calls      int
}

func (s *stubPlacements) EntryPlacements(entryIDs []string) (map[string]conversation.EntryPlacement, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	out := map[string]conversation.EntryPlacement{}
	for _, id := range entryIDs {
		if p, ok := s.placements[id]; ok {
			out[id] = p
		}
	}
	return out, nil
}

type batchAssignments struct {
	stubAssignments
	byEntry map[string]*inbox_assignment.InboxAssignment
	batches int
	singles int
}

func (s *batchAssignments) FindByEntry(string, string, string) (*inbox_assignment.InboxAssignment, error) {
	s.singles++
	return nil, nil
}

func (s *batchAssignments) FindByEntries(_ string, entryIDs []string) ([]*inbox_assignment.InboxAssignment, error) {
	s.batches++
	var out []*inbox_assignment.InboxAssignment
	for _, id := range entryIDs {
		if a, ok := s.byEntry[id]; ok {
			out = append(out, a)
		}
	}
	return out, nil
}

type visibilityFixture struct {
	authorizer  *Authorizer
	placements  *stubPlacements
	assignments *batchAssignments
	members     *countingMembership
	departments *countingDepartments
	refs        []shared.EntryRef
}

func entryID(i int) string { return fmt.Sprintf("entry-%03d", i) }

func newVisibilityFixture(viewOthers bool) *visibilityFixture {
	perms := map[workspace.Action]bool{}
	if viewOthers {
		perms[workspace.ActionViewOthers] = true
	}
	f := &visibilityFixture{
		placements:  &stubPlacements{placements: map[string]conversation.EntryPlacement{}},
		assignments: &batchAssignments{byEntry: map[string]*inbox_assignment.InboxAssignment{}},
		members:     &countingMembership{stubMembership: stubMembership{permissions: perms}},
		departments: &countingDepartments{},
	}
	for i := 0; i < 500; i++ {
		id := entryID(i)
		department := testDepartment
		if i >= 250 {
			department = "dept-2"
		}
		f.placements.placements[id] = conversation.EntryPlacement{WorkspaceID: testWorkspace, DepartmentID: department}
		f.refs = append(f.refs, shared.EntryRef{EntryID: id, EntryType: shared.EntryTypeWhatsApp})
	}
	for i := 0; i < 10; i++ {
		f.assignments.byEntry[entryID(i)] = &inbox_assignment.InboxAssignment{WorkspaceID: testWorkspace, EntryID: entryID(i), EntryType: testEntryType, AssignedUserID: owner}
	}
	for i := 400; i < 410; i++ {
		f.assignments.byEntry[entryID(i)] = &inbox_assignment.InboxAssignment{WorkspaceID: testWorkspace, EntryID: entryID(i), EntryType: testEntryType, AssignedUserID: viewer}
	}
	f.authorizer = NewAuthorizer(
		stubEntryAccess{},
		f.placements,
		f.members,
		f.departments,
		f.assignments,
		stubResolver{},
		&stubShared{data: map[string]string{}},
	).(*Authorizer)
	return f
}

func visibleCount(visible map[shared.EntryRef]bool) int {
	n := 0
	for _, ok := range visible {
		if ok {
			n++
		}
	}
	return n
}

func TestVisibleEntriesAnswersFiveHundredEntriesWithAConstantNumberOfReads(t *testing.T) {
	f := newVisibilityFixture(true)
	visible, err := f.authorizer.EntryVisibilityFor(viewer, testWorkspace, false).VisibleEntries(f.refs)
	if err != nil {
		t.Fatalf("VisibleEntries: %v", err)
	}
	if got := visibleCount(visible); got != 260 {
		t.Fatalf("visible = %d, want the 250 entries of the caller department plus the 10 assigned to the caller", got)
	}
	if !visible[f.refs[0]] || !visible[f.refs[405]] || visible[f.refs[300]] {
		t.Fatal("department and assignment decide each entry")
	}
	if f.placements.calls != 1 || f.assignments.batches != 1 || f.assignments.singles != 0 || f.members.members > 2 || f.departments.lists != 1 {
		t.Fatalf("reads: placements %d, assignment batches %d, single assignments %d, members %d, department lists %d",
			f.placements.calls, f.assignments.batches, f.assignments.singles, f.members.members, f.departments.lists)
	}
}

func TestVisibleEntriesHidesAConversationAssignedToSomeoneElseWithoutViewOthers(t *testing.T) {
	f := newVisibilityFixture(false)
	visible, err := f.authorizer.EntryVisibilityFor(viewer, testWorkspace, false).VisibleEntries(f.refs)
	if err != nil {
		t.Fatalf("VisibleEntries: %v", err)
	}
	if visible[f.refs[0]] || !visible[f.refs[10]] || visibleCount(visible) != 250 {
		t.Fatalf("visible = %d, entry 0 %v, entry 10 %v", visibleCount(visible), visible[f.refs[0]], visible[f.refs[10]])
	}
}

func TestVisibleEntriesHidesEntriesOfAnotherWorkspaceAndEntriesThatAreGone(t *testing.T) {
	f := newVisibilityFixture(true)
	f.placements.placements[entryID(1)] = conversation.EntryPlacement{WorkspaceID: "ws-2", DepartmentID: testDepartment}
	delete(f.placements.placements, entryID(2))
	visible, err := f.authorizer.EntryVisibilityFor(viewer, testWorkspace, false).VisibleEntries(f.refs[:3])
	if err != nil {
		t.Fatalf("VisibleEntries: %v", err)
	}
	if !visible[f.refs[0]] || visible[f.refs[1]] || visible[f.refs[2]] {
		t.Fatalf("visible = %v", visible)
	}
}

func TestVisibleEntriesFailsWhenThePlacementsCannotBeRead(t *testing.T) {
	f := newVisibilityFixture(true)
	f.placements.err = errors.New("database down")
	if _, err := f.authorizer.EntryVisibilityFor(viewer, testWorkspace, false).VisibleEntries(f.refs); err == nil {
		t.Fatal("an unreadable placement must fail the check, not hide or show everything")
	}
}

func TestVisibleEntriesForAPlatformAdminNeedsOnlyThePlacements(t *testing.T) {
	f := newVisibilityFixture(false)
	f.placements.placements[entryID(1)] = conversation.EntryPlacement{WorkspaceID: "ws-2"}
	visible, err := f.authorizer.EntryVisibilityFor(viewer, testWorkspace, true).VisibleEntries(f.refs)
	if err != nil {
		t.Fatalf("VisibleEntries: %v", err)
	}
	if visibleCount(visible) != 500 || f.assignments.batches != 0 || f.placements.calls != 1 {
		t.Fatalf("visible = %d, assignment batches %d, placements %d", visibleCount(visible), f.assignments.batches, f.placements.calls)
	}
}

func TestVisibleEntriesRefusesWithoutACallerOrWorkspace(t *testing.T) {
	f := newVisibilityFixture(true)
	for _, access := range []conversation.EntryVisibility{
		f.authorizer.EntryVisibilityFor("", testWorkspace, false),
		f.authorizer.EntryVisibilityFor(viewer, "", false),
	} {
		visible, err := access.VisibleEntries(f.refs[:5])
		if err != nil || visibleCount(visible) != 0 {
			t.Fatalf("visible = %v, %v", visible, err)
		}
	}
	if f.placements.calls != 0 {
		t.Fatal("nothing is read for a caller without identity")
	}
}

func TestVisibleEntriesChecksOtherChannelsOneConversationAtATime(t *testing.T) {
	f := newVisibilityFixture(true)
	refs := []shared.EntryRef{{EntryID: "telegram-1", EntryType: shared.EntryTypeTelegram}, {EntryID: "", EntryType: shared.EntryTypeWhatsApp}}
	visible, err := f.authorizer.EntryVisibilityFor(viewer, testWorkspace, false).VisibleEntries(refs)
	if err != nil {
		t.Fatalf("VisibleEntries: %v", err)
	}
	if visible[refs[0]] || visible[refs[1]] {
		t.Fatalf("a channel without an access repository and an empty id are hidden, got %v", visible)
	}
}
