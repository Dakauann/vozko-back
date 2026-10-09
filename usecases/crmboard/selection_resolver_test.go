package crmboard_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/conversation"
	"vozko/domain/crmfilter"
	"vozko/domain/selection"
	"vozko/domain/shared"
)

func scopedAuthorizer() *fakeAuthorizer {
	return &fakeAuthorizer{allowed: true, viewOthers: true, scope: conversation.DepartmentAccessScope{Restrict: true, DepartmentIDs: []string{"d1", "d2"}}}
}

func TestSelectionResolver_ResolvesAKeysetPageInTheActorScope(t *testing.T) {
	searcher := &fakeRefSearcher{refs: []shared.EntryRef{{EntryID: "e1", EntryType: "whatsapp"}, {EntryID: "e2", EntryType: "instagram"}}}
	resolver := NewSelectionResolver(NewService(searcher, &fakeStages{}, &fakeLabels{}, scopedAuthorizer(), &fakeAssignments{}))
	filter := unreadFilter()
	sel := selection.Selection{Mode: selection.ModeAllMatching, Filter: &filter, ExcludeIDs: []string{"e9"}}

	refs, err := resolver.Resolve(context.Background(), selection.Scope{WorkspaceID: "ws", ActorID: "u1", DepartmentID: "d2"}, sel, "e0", 50)

	if err != nil || len(refs) != 2 || refs[1] != (selection.Ref{ID: "e2", Type: "instagram"}) {
		t.Fatalf("got %+v, %v", refs, err)
	}
	got := searcher.refCalls[0]
	if got.WorkspaceID != "ws" || got.DepartmentIDs[0] != "d2" || got.Filter.IsEmpty() || len(got.ExcludeEntryIDs) != 1 {
		t.Fatalf("selection not translated: %+v", got)
	}
	if searcher.afters[0] != "e0" || searcher.limits[0] != 50 {
		t.Fatalf("keyset not forwarded: %v %v", searcher.afters, searcher.limits)
	}
}

func TestSelectionResolver_CountsTheEffectiveFilter(t *testing.T) {
	searcher := &fakeRefSearcher{countResult: 41}
	resolver := NewSelectionResolver(NewService(searcher, &fakeStages{}, &fakeLabels{}, scopedAuthorizer(), &fakeAssignments{}))
	stale := unreadFilter()

	matched, err := resolver.Count(context.Background(), selection.Scope{WorkspaceID: "ws", ActorID: "u1"}, selection.Selection{Mode: selection.ModeEveryone, Filter: &stale})

	if err != nil || matched != 41 {
		t.Fatalf("got %d, %v", matched, err)
	}
	if !searcher.countCalls[0].Filter.IsEmpty() {
		t.Fatal("everyone counts the whole scope, never a filter it carries")
	}
}

func TestSelectionResolver_ScopeRefusalsReadAsSelectionScopeDenied(t *testing.T) {
	cases := []struct {
		name  string
		auth  Authorizer
		scope selection.Scope
	}{
		{"no authorizer", nil, selection.Scope{WorkspaceID: "ws", ActorID: "u1"}},
		{"no access", &fakeAuthorizer{allowed: false}, selection.Scope{WorkspaceID: "ws", ActorID: "u1"}},
		{"a department outside the scope", scopedAuthorizer(), selection.Scope{WorkspaceID: "ws", ActorID: "u1", DepartmentID: "d9"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			searcher := &fakeRefSearcher{}
			resolver := NewSelectionResolver(NewService(searcher, &fakeStages{}, &fakeLabels{}, tc.auth, &fakeAssignments{}))
			sel := selection.Selection{Mode: selection.ModeEveryone}

			if _, err := resolver.Count(context.Background(), tc.scope, sel); !errors.Is(err, selection.ErrScopeDenied) {
				t.Fatalf("count: want ErrScopeDenied, got %v", err)
			}
			if _, err := resolver.Resolve(context.Background(), tc.scope, sel, "", 10); !errors.Is(err, selection.ErrScopeDenied) {
				t.Fatalf("resolve: want ErrScopeDenied, got %v", err)
			}
			if len(searcher.refCalls)+len(searcher.countCalls) != 0 {
				t.Fatal("a refused scope reads nothing")
			}
		})
	}
}

func TestSelectionResolver_WithoutABoardRefuses(t *testing.T) {
	resolver := NewSelectionResolver(nil)
	if _, err := resolver.Count(context.Background(), selection.Scope{}, selection.Selection{}); !errors.Is(err, selection.ErrResolverUnavailable) {
		t.Fatalf("count: want ErrResolverUnavailable, got %v", err)
	}
	if _, err := resolver.Resolve(context.Background(), selection.Scope{}, selection.Selection{}, "", 10); !errors.Is(err, selection.ErrResolverUnavailable) {
		t.Fatalf("resolve: want ErrResolverUnavailable, got %v", err)
	}
}

func TestSelectionResolver_PassesOtherErrorsThrough(t *testing.T) {
	boom := errors.New("db down")
	searcher := &fakeRefSearcher{}
	searcher.err = boom
	resolver := NewSelectionResolver(NewService(searcher, &fakeStages{}, &fakeLabels{}, scopedAuthorizer(), &fakeAssignments{}))
	filter := crmfilter.Filter{}

	if _, err := resolver.Count(context.Background(), selection.Scope{WorkspaceID: "ws", ActorID: "u1"}, selection.Selection{Mode: selection.ModeEveryone, Filter: &filter}); !errors.Is(err, boom) || errors.Is(err, selection.ErrScopeDenied) {
		t.Fatalf("want the read error untouched, got %v", err)
	}
}
