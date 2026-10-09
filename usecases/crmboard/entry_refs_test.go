package crmboard_usecase

import (
	"errors"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/crmfilter"
	"vozko/domain/shared"
)

type fakeRefSearcher struct {
	fakeSearcher
	refs        []shared.EntryRef
	refCalls    []conversation.SearchByFilterInput
	afters      []string
	limits      []int
	countCalls  []conversation.SearchByFilterInput
	countResult int64
}

func (f *fakeRefSearcher) CountEntriesByFilter(in conversation.SearchByFilterInput) (int64, error) {
	f.countCalls = append(f.countCalls, in)
	return f.countResult, f.err
}

func (f *fakeRefSearcher) ResolveEntryRefsByFilter(in conversation.SearchByFilterInput, after string, limit int) ([]shared.EntryRef, error) {
	f.refCalls = append(f.refCalls, in)
	f.afters = append(f.afters, after)
	f.limits = append(f.limits, limit)
	return f.refs, f.err
}

func unreadFilter() crmfilter.Filter {
	return crmfilter.Filter{Groups: []crmfilter.Group{{
		Conjunction: crmfilter.And,
		Predicates:  []crmfilter.Predicate{{Field: crmfilter.FieldUnread, Operator: crmfilter.OpIsTrue}},
	}}}
}

func TestResolveEntryRefs_AppliesTheActorScope(t *testing.T) {
	cases := []struct {
		name          string
		auth          *fakeAuthorizer
		in            EntriesInput
		wantRestrict  bool
		wantDepts     []string
		wantOverride  string
		wantAssignee  string
		wantForbidden bool
	}{
		{
			name:         "restricted member sees own departments and own assignments",
			auth:         &fakeAuthorizer{allowed: true, scope: conversation.DepartmentAccessScope{Restrict: true, DepartmentIDs: []string{"d1"}}},
			in:           EntriesInput{WorkspaceID: "ws", UserID: "u1"},
			wantRestrict: true, wantDepts: []string{"d1"}, wantOverride: "u1", wantAssignee: "u1",
		},
		{
			name:         "member with view others sees the department queue",
			auth:         &fakeAuthorizer{allowed: true, viewOthers: true, scope: conversation.DepartmentAccessScope{Restrict: true, DepartmentIDs: []string{"d1", "d2"}}},
			in:           EntriesInput{WorkspaceID: "ws", UserID: "u1", SelectedDepartmentID: "d2"},
			wantRestrict: true, wantDepts: []string{"d2"}, wantOverride: "u1",
		},
		{
			name:          "picking a department outside the scope is refused",
			auth:          &fakeAuthorizer{allowed: true, scope: conversation.DepartmentAccessScope{Restrict: true, DepartmentIDs: []string{"d1"}}},
			in:            EntriesInput{WorkspaceID: "ws", UserID: "u1", SelectedDepartmentID: "d9"},
			wantForbidden: true,
		},
		{
			name:          "a member without access is refused",
			auth:          &fakeAuthorizer{allowed: false},
			in:            EntriesInput{WorkspaceID: "ws", UserID: "u1"},
			wantForbidden: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			searcher := &fakeRefSearcher{refs: []shared.EntryRef{{EntryID: "e1", EntryType: "whatsapp"}}}
			svc := NewService(searcher, &fakeStages{}, &fakeLabels{}, tc.auth, &fakeAssignments{})
			tc.in.Filter = unreadFilter()
			tc.in.ExcludeEntryIDs = []string{"e9"}

			refs, err := svc.ResolveEntryRefs(tc.in, "e0", 50)
			if tc.wantForbidden {
				if !errors.Is(err, ErrUnauthorized) || len(searcher.refCalls) != 0 {
					t.Fatalf("want ErrUnauthorized before any read, got %v / %d reads", err, len(searcher.refCalls))
				}
				return
			}
			if err != nil || len(refs) != 1 {
				t.Fatalf("got %v %v", refs, err)
			}
			got := searcher.refCalls[0]
			if got.RestrictDepartments != tc.wantRestrict || got.AssigneeOverrideUserID != tc.wantOverride || got.AssignedUserID != tc.wantAssignee {
				t.Fatalf("scope not applied: %+v", got)
			}
			if len(got.DepartmentIDs) != len(tc.wantDepts) || (len(tc.wantDepts) > 0 && got.DepartmentIDs[0] != tc.wantDepts[0]) {
				t.Fatalf("departments %v, want %v", got.DepartmentIDs, tc.wantDepts)
			}
			if got.WorkspaceID != "ws" || len(got.ExcludeEntryIDs) != 1 || got.Filter.IsEmpty() {
				t.Fatalf("selection not forwarded: %+v", got)
			}
			if searcher.afters[0] != "e0" || searcher.limits[0] != 50 {
				t.Fatalf("keyset not forwarded: %v %v", searcher.afters, searcher.limits)
			}
		})
	}
}

func TestCountEntries_UsesTheSameScope(t *testing.T) {
	searcher := &fakeRefSearcher{countResult: 12}
	auth := &fakeAuthorizer{allowed: true, scope: conversation.DepartmentAccessScope{Restrict: true, DepartmentIDs: []string{"d1"}}}
	svc := NewService(searcher, &fakeStages{}, &fakeLabels{}, auth, &fakeAssignments{})

	got, err := svc.CountEntries(EntriesInput{WorkspaceID: "ws", UserID: "u1", Filter: unreadFilter()})
	if err != nil || got != 12 {
		t.Fatalf("got %d, %v", got, err)
	}
	if c := searcher.countCalls[0]; !c.RestrictDepartments || c.AssigneeOverrideUserID != "u1" {
		t.Fatalf("count must carry the actor scope: %+v", c)
	}
}

func TestEntryReads_RefuseWithoutAnAuthorizer(t *testing.T) {
	searcher := &fakeRefSearcher{}
	svc := NewService(searcher, &fakeStages{}, &fakeLabels{}, nil, &fakeAssignments{})

	if _, err := svc.ResolveEntryRefs(EntriesInput{WorkspaceID: "ws", UserID: "u1"}, "", 10); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("want ErrUnauthorized, got %v", err)
	}
	if _, err := svc.CountEntries(EntriesInput{WorkspaceID: "ws", UserID: "u1"}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("want ErrUnauthorized, got %v", err)
	}
	if _, _, err := svc.GetEntries(EntriesInput{WorkspaceID: "ws", UserID: "u1"}); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("want ErrUnauthorized, got %v", err)
	}
	if len(searcher.calls)+len(searcher.refCalls)+len(searcher.countCalls) != 0 {
		t.Fatal("nothing may be read without an authorizer")
	}
}

func (f *fakeSearcher) CountEntriesByFilter(in conversation.SearchByFilterInput) (int64, error) {
	f.calls = append(f.calls, in)
	return f.total, f.err
}

func (f *fakeSearcher) ResolveEntryRefsByFilter(in conversation.SearchByFilterInput, _ string, _ int) ([]shared.EntryRef, error) {
	f.calls = append(f.calls, in)
	return nil, f.err
}
