package teamchat

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var created = time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)

func TestTheDirectKeyIsTheSameWhoeverStartsTheThread(t *testing.T) {
	if DirectKey("b", "a") != DirectKey("a", "b") {
		t.Fatalf("keys differ: %q and %q", DirectKey("b", "a"), DirectKey("a", "b"))
	}
	if DirectKey("a", "b") != "a:b" {
		t.Fatalf("key = %q", DirectKey("a", "b"))
	}
}

func TestNewDirect(t *testing.T) {
	cases := []struct {
		name      string
		workspace string
		me        string
		other     string
		want      error
	}{
		{"two people", "ws", "ana", "bia", nil},
		{"no workspace", " ", "ana", "bia", ErrWorkspaceRequired},
		{"no me", "ws", "", "bia", ErrPersonRequired},
		{"no other", "ws", "ana", " ", ErrPersonRequired},
		{"talking to yourself", "ws", "ana", " ana ", ErrSamePerson},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			thread, members, err := NewDirect("t1", tc.workspace, tc.me, tc.other, created)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if err != nil {
				return
			}
			if thread.Kind != KindDirect || thread.DirectKey != "ana:bia" || thread.Title != "" {
				t.Fatalf("thread = %+v", thread)
			}
			if !thread.LastMessageAt.Equal(created) {
				t.Fatalf("a new thread sorts by its creation, got %v", thread.LastMessageAt)
			}
			if len(members) != 2 || members[0].Role != RoleMember || members[1].Role != RoleMember {
				t.Fatalf("a direct thread has two plain members, got %+v", members)
			}
		})
	}
}

func TestNewGroup(t *testing.T) {
	many := make([]string, MaxGroupMembers)
	for i := range many {
		many[i] = "p" + strings.Repeat("x", i%7) + string(rune('a'+i%26)) + string(rune('a'+i/26))
	}
	cases := []struct {
		name    string
		title   string
		others  []string
		want    error
		members int
	}{
		{"creator and two people", " Vendas ", []string{"bia", "caio"}, nil, 3},
		{"repeated and blank people count once", "Vendas", []string{"bia", " bia ", "", "ana"}, nil, 2},
		{"no title", "  ", []string{"bia"}, ErrTitleRequired, 0},
		{"title too long", strings.Repeat("é", MaxTitleRunes+1), []string{"bia"}, ErrTitleTooLong, 0},
		{"nobody else", "Vendas", []string{"ana", " "}, ErrMembersRequired, 0},
		{"over the member limit", "Vendas", many, ErrTooManyMembers, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			thread, members, err := NewGroup("t1", "ws", "ana", tc.title, tc.others, created)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if err != nil {
				return
			}
			if thread.Kind != KindGroup || thread.Title != "Vendas" || thread.DirectKey != "" || thread.CreatedBy != "ana" {
				t.Fatalf("thread = %+v", thread)
			}
			if len(members) != tc.members {
				t.Fatalf("members = %d, want %d", len(members), tc.members)
			}
			if members[0].UserID != "ana" || members[0].Role != RoleAdmin {
				t.Fatalf("the creator is the first admin, got %+v", members[0])
			}
			for _, m := range members[1:] {
				if m.Role != RoleMember {
					t.Fatalf("other people join as members, got %+v", m)
				}
			}
		})
	}
}

func TestOnlyGroupAdminsManageAGroup(t *testing.T) {
	group := Thread{Kind: KindGroup}
	direct := Thread{Kind: KindDirect}
	admin := Member{UserID: "ana", Role: RoleAdmin}
	member := Member{UserID: "bia", Role: RoleMember}
	if err := group.ManagedBy(admin); err != nil {
		t.Fatalf("admin: %v", err)
	}
	if err := group.ManagedBy(member); !errors.Is(err, ErrNotAdmin) {
		t.Fatalf("member: %v", err)
	}
	if err := direct.ManagedBy(admin); !errors.Is(err, ErrDirectThread) {
		t.Fatalf("direct: %v", err)
	}
}

func TestRenamedKeepsTheTitleRules(t *testing.T) {
	group := Thread{Kind: KindGroup, Title: "Vendas"}
	renamed, err := group.Renamed("  Vendas Sul ")
	if err != nil || renamed.Title != "Vendas Sul" {
		t.Fatalf("renamed = %+v, err = %v", renamed, err)
	}
	if _, err := group.Renamed(""); !errors.Is(err, ErrTitleRequired) {
		t.Fatalf("empty title: %v", err)
	}
	if _, err := (Thread{Kind: KindDirect}).Renamed("x"); !errors.Is(err, ErrDirectThread) {
		t.Fatalf("direct: %v", err)
	}
}
