package teamchat

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func left(at time.Time) *time.Time { return &at }

func TestUnreadNeverGoesNegative(t *testing.T) {
	m := Member{LastReadSeq: 7}
	if m.Unread(10) != 3 {
		t.Fatalf("unread = %d", m.Unread(10))
	}
	if m.Unread(5) != 0 {
		t.Fatalf("unread past the end = %d", m.Unread(5))
	}
}

func TestReadUpToNeverMovesBackOrPastTheLastMessage(t *testing.T) {
	cases := []struct {
		current, requested, last, want int64
	}{
		{3, 8, 10, 8},
		{8, 3, 10, 8},
		{3, 50, 10, 10},
		{3, -1, 10, 3},
	}
	for _, tc := range cases {
		if got := ReadUpTo(tc.current, tc.requested, tc.last); got != tc.want {
			t.Errorf("ReadUpTo(%d, %d, %d) = %d, want %d", tc.current, tc.requested, tc.last, got, tc.want)
		}
	}
}

func roster() Roster {
	return Roster{
		{UserID: "ana", Role: RoleAdmin, JoinedAt: created},
		{UserID: "bia", Role: RoleMember, JoinedAt: created.Add(2 * time.Minute)},
		{UserID: "caio", Role: RoleMember, JoinedAt: created.Add(time.Minute)},
		{UserID: "dani", Role: RoleAdmin, JoinedAt: created, LeftAt: left(created.Add(time.Hour))},
	}
}

func TestRosterActiveIgnoresPeopleWhoLeft(t *testing.T) {
	r := roster()
	if got := r.ActiveUserIDs(); !reflect.DeepEqual(got, []string{"ana", "bia", "caio"}) {
		t.Fatalf("active = %v", got)
	}
	if _, ok := r.Active("dani"); ok {
		t.Fatal("dani left and must not count as a member")
	}
	if m, ok := r.Active("bia"); !ok || m.UserID != "bia" {
		t.Fatalf("bia = %+v, %v", m, ok)
	}
}

func TestOtherIsTheSecondPersonOfADirectThread(t *testing.T) {
	r := Roster{{UserID: "ana"}, {UserID: "bia"}}
	if other, ok := r.Other("ana"); !ok || other.UserID != "bia" {
		t.Fatalf("other = %+v, %v", other, ok)
	}
}

func TestTheEarliestMemberInheritsTheGroupWhenTheLastAdminLeaves(t *testing.T) {
	r := roster()
	successor, ok := r.SuccessorAdmin("ana")
	if !ok || successor != "caio" {
		t.Fatalf("successor = %q, %v", successor, ok)
	}
	r[1].Role = RoleAdmin
	if _, ok := r.SuccessorAdmin("ana"); ok {
		t.Fatal("bia is still an admin, nobody needs promoting")
	}
	if _, ok := r.SuccessorAdmin("caio"); ok {
		t.Fatal("a member leaving promotes nobody")
	}
	alone := Roster{{UserID: "ana", Role: RoleAdmin}}
	if _, ok := alone.SuccessorAdmin("ana"); ok {
		t.Fatal("nobody is left to promote")
	}
}

func TestJoiningMembers(t *testing.T) {
	group := Thread{ID: "t1", WorkspaceID: "ws", Kind: KindGroup, LastSeq: 40}
	joining, err := roster().Joining(group, []string{"bia", "dani", "eva", " eva ", ""}, created)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for _, m := range joining {
		ids = append(ids, m.UserID)
		if m.Role != RoleMember || m.LeftAt != nil || m.LastReadSeq != 40 || !m.JoinedAt.Equal(created) || m.ThreadID != "t1" || m.WorkspaceID != "ws" {
			t.Fatalf("joining = %+v", m)
		}
	}
	if !reflect.DeepEqual(ids, []string{"dani", "eva"}) {
		t.Fatalf("only people outside the group join, got %v", ids)
	}
	if _, err := roster().Joining(group, []string{"bia"}, created); !errors.Is(err, ErrMembersRequired) {
		t.Fatalf("nobody new: %v", err)
	}
	full := make(Roster, MaxGroupMembers)
	for i := range full {
		full[i] = Member{UserID: string(rune('a'+i%26)) + string(rune('a'+i/26))}
	}
	if _, err := full.Joining(group, []string{"zz9"}, created); !errors.Is(err, ErrTooManyMembers) {
		t.Fatalf("full group: %v", err)
	}
}

func TestLeavingMarksTheMemberGoneAndPromotesASuccessor(t *testing.T) {
	changes, err := roster().Leaving("ana", created)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 2 {
		t.Fatalf("changes = %+v", changes)
	}
	if changes[0].UserID != "ana" || changes[0].LeftAt == nil || !changes[0].LeftAt.Equal(created) {
		t.Fatalf("leaving = %+v", changes[0])
	}
	if changes[1].UserID != "caio" || changes[1].Role != RoleAdmin {
		t.Fatalf("successor = %+v", changes[1])
	}
	if _, err := roster().Leaving("dani", created); !errors.Is(err, ErrNotFound) {
		t.Fatalf("someone who already left: %v", err)
	}
}
