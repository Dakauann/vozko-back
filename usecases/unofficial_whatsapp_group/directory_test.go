package unofficial_whatsapp_group_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
	uwg "vozko/domain/unofficial_whatsapp_group"
)

func TestDiscoverListsTheGroupsOfConnectedNumbersInScope(t *testing.T) {
	w := newWorld()
	w.instances.instances = append(w.instances.instances, &uw.Instance{ID: "n3", WorkspaceID: ws, InstanceToken: "n3", Status: uw.StatusDisconnected})
	w.api.listed["n1"] = []*uw.Group{{JID: "120363000000000001@g.us", Subject: "Moradores", ParticipantCount: 287}}
	w.api.listed["n2"] = []*uw.Group{{JID: "120363000000000002@g.us", Subject: "Ofertas"}}
	w.api.listErr["n2"] = errors.New("host down")

	result, err := w.svc.Discover(context.Background(), everyone())
	if err != nil {
		t.Fatal(err)
	}
	if result.Numbers != 2 || result.Groups != 1 || len(result.Failed) != 1 || result.Failed[0] != "n2" {
		t.Fatalf("discover = %+v, want two connected numbers tried, one group found, one failure reported", result)
	}
	g, err := w.groups.FindByJID(context.Background(), ws, "120363000000000001@g.us")
	if err != nil || g.PrimaryInstanceID != "n1" || g.MemberCount != 287 || g.Followed() {
		t.Fatalf("discovered group = %+v, %v", g, err)
	}
	if _, err := w.seats.FindByJID(context.Background(), "n1", "120363000000000001@g.us"); err != nil {
		t.Fatalf("the number's seat was not stored: %v", err)
	}
}

func TestDiscoverOnlyAsksTheCallersNumbers(t *testing.T) {
	w := newWorld()
	w.api.listed["n1"] = []*uw.Group{{JID: "120363000000000001@g.us", Subject: "Moradores"}}
	w.api.listed["n2"] = []*uw.Group{{JID: "120363000000000002@g.us", Subject: "Ofertas"}}

	result, err := w.svc.Discover(context.Background(), salesOnly())
	if err != nil || result.Numbers != 1 || result.Groups != 1 {
		t.Fatalf("scoped discover = %+v, %v; want only the sales number", result, err)
	}
	if _, err := w.groups.FindByJID(context.Background(), ws, "120363000000000002@g.us"); err == nil {
		t.Fatalf("a number outside the scope was listed")
	}
}

func TestListGroupsShowsOnlyGroupsOfNumbersInScopeWithTheirSeats(t *testing.T) {
	w := newWorld()
	w.groupWithSeats(t, "120363000000000001@g.us", "Moradores", "n1", "n2")
	w.groupWithSeats(t, "120363000000000002@g.us", "Ofertas", "n2")

	page, err := w.svc.ListGroups(context.Background(), salesOnly(), ListGroupsInput{Pagination: shared.Pagination{Page: 1, PageSize: 20}})
	if err != nil {
		t.Fatal(err)
	}
	if page.TotalItems != 1 || page.Items[0].Group.Subject != "Moradores" {
		t.Fatalf("scoped list = %+v", page.Items)
	}
	if len(page.Items[0].Numbers) != 1 || page.Items[0].Numbers[0].InstanceID != "n1" {
		t.Fatalf("numbers = %+v, want only the caller's number shown", page.Items[0].Numbers)
	}
}

func TestGroupsByIDRefusesTheWholeListWhenOneIsOutOfScope(t *testing.T) {
	w := newWorld()
	mine := w.groupWithSeats(t, "120363000000000001@g.us", "Moradores", "n1", "n2")
	theirs := w.groupWithSeats(t, "120363000000000002@g.us", "Ofertas", "n2")

	rows, err := w.svc.GroupsByID(context.Background(), salesOnly(), []string{mine.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || len(rows[0].Numbers) != 1 || rows[0].Numbers[0].InstanceID != "n1" || !rows[0].Numbers[0].CanPost {
		t.Fatalf("rows = %+v, want the group with only the caller's number", rows)
	}
	if _, err := w.svc.GroupsByID(context.Background(), salesOnly(), []string{mine.ID, theirs.ID}); !errors.Is(err, uwg.ErrGroupNotFound) {
		t.Fatalf("GroupsByID with another department's group = %v, want not found", err)
	}
}

func TestAGroupOutsideTheScopeIsNotFound(t *testing.T) {
	w := newWorld()
	g := w.groupWithSeats(t, "120363000000000002@g.us", "Ofertas", "n2")

	if _, err := w.svc.GetGroup(context.Background(), salesOnly(), g.ID); !errors.Is(err, uwg.ErrGroupNotFound) {
		t.Fatalf("GetGroup = %v, want not found", err)
	}
	if _, err := w.svc.Follow(context.Background(), salesOnly(), g.ID); !errors.Is(err, uwg.ErrGroupNotFound) {
		t.Fatalf("Follow = %v, want not found", err)
	}
}

func TestFollowingTakesTheRosterAsABaselineWithoutGrowth(t *testing.T) {
	w := newWorld()
	g := w.groupWithSeats(t, "120363000000000001@g.us", "Moradores", "n1")
	w.seatSync.rosters[g.JID] = participants("5511981124410", "5511992307781", "5511970001201")

	followed, err := w.svc.Follow(context.Background(), everyone(), g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !followed.Followed() || followed.MembersSyncedAt == nil {
		t.Fatalf("followed = %+v", followed)
	}
	if w.groups.get(g.ID).MemberCount != 2 {
		t.Fatalf("member count = %d, want our own number left out", w.groups.get(g.ID).MemberCount)
	}
	if w.members.eventsOf(uwg.MemberEventJoin) != 0 {
		t.Fatalf("the first roster was recorded as joins")
	}
	if len(w.seatSync.refreshs) != 1 || w.seatSync.refreshs[0] != "n1|"+g.JID {
		t.Fatalf("refreshes = %v, want the primary seat refreshed", w.seatSync.refreshs)
	}
}

func TestUnfollowingKeepsHistoryAndStartsANewBaselineNextTime(t *testing.T) {
	w := newWorld()
	g := w.groupWithSeats(t, "120363000000000001@g.us", "Moradores", "n1")
	w.seatSync.rosters[g.JID] = participants("5511981124410")
	if _, err := w.svc.Follow(context.Background(), everyone(), g.ID); err != nil {
		t.Fatal(err)
	}
	unfollowed, err := w.svc.Unfollow(context.Background(), everyone(), g.ID)
	if err != nil || unfollowed.Followed() || unfollowed.MembersSyncedAt != nil {
		t.Fatalf("unfollow = %+v, %v", unfollowed, err)
	}
	w.seatSync.rosters[g.JID] = participants("5511960213375")
	if _, err := w.svc.Follow(context.Background(), everyone(), g.ID); err != nil {
		t.Fatal(err)
	}
	if w.members.eventsOf(uwg.MemberEventJoin) != 0 || w.members.eventsOf(uwg.MemberEventLeave) != 0 {
		t.Fatalf("following again recorded what happened while unfollowed")
	}
}

func TestRenamingNeedsANumberThatIsAdmin(t *testing.T) {
	w := newWorld()
	g := w.groupWithSeats(t, "120363000000000001@g.us", "Moradores", "n1", "n2")
	name := "Moradores VM"

	if _, err := w.svc.UpdateGroup(context.Background(), everyone(), g.ID, UpdateGroupInput{Subject: &name}); !errors.Is(err, uw.ErrNotGroupAdmin) {
		t.Fatalf("rename without an admin seat = %v, want ErrNotGroupAdmin", err)
	}

	seat, _ := w.seats.FindByJID(context.Background(), "n2", g.JID)
	seat.WeAreAdmin = true
	if _, err := w.svc.UpdateGroup(context.Background(), everyone(), g.ID, UpdateGroupInput{Subject: &name}); err != nil {
		t.Fatal(err)
	}
	if len(w.seatSync.renamed) != 1 || w.seatSync.renamed[0] != "n2|Moradores VM" {
		t.Fatalf("renamed = %v, want the admin number used", w.seatSync.renamed)
	}
	if _, err := w.svc.UpdateGroup(context.Background(), salesOnly(), g.ID, UpdateGroupInput{Subject: &name}); !errors.Is(err, uw.ErrNotGroupAdmin) {
		t.Fatalf("rename through an admin seat outside the scope = %v, want ErrNotGroupAdmin", err)
	}
}

func TestCategoryIsSavedWithoutTouchingWhatsApp(t *testing.T) {
	w := newWorld()
	g := w.groupWithSeats(t, "120363000000000001@g.us", "Moradores", "n1")
	category := " Bairro "

	updated, err := w.svc.UpdateGroup(context.Background(), everyone(), g.ID, UpdateGroupInput{Category: &category})
	if err != nil || updated.Category != "Bairro" {
		t.Fatalf("update = %+v, %v", updated, err)
	}
	if len(w.seatSync.renamed) != 0 {
		t.Fatalf("a category change reached WhatsApp")
	}
}

func TestLeavingOnlyUsesTheCallersNumbers(t *testing.T) {
	w := newWorld()
	g := w.groupWithSeats(t, "120363000000000001@g.us", "Moradores", "n1", "n2")

	if err := w.svc.LeaveGroup(context.Background(), salesOnly(), g.ID, []string{"n2"}); !errors.Is(err, uwg.ErrNumberOutOfScope) {
		t.Fatalf("leave with another department's number = %v, want ErrNumberOutOfScope", err)
	}
	if err := w.svc.LeaveGroup(context.Background(), salesOnly(), g.ID, []string{"n1"}); err != nil {
		t.Fatal(err)
	}
	if len(w.seatSync.left) != 1 || w.seatSync.left[0] != "n1" {
		t.Fatalf("left = %v", w.seatSync.left)
	}
	if got := w.groups.get(g.ID).PrimaryInstanceID; got != "n2" {
		t.Fatalf("primary after leaving = %s, want the remaining number", got)
	}
}

func TestAnUnfollowDuringAWhatsAppRefreshStays(t *testing.T) {
	w := newWorld()
	g := w.followed(t, "120363000000000001@g.us", participants("5511981124410"), "n1")
	w.seatSync.duringGet = func() {
		if _, err := w.svc.Unfollow(context.Background(), everyone(), g.ID); err != nil {
			t.Error(err)
		}
	}

	w.svc.GroupChanged(context.Background(), w.instance("n1"), g.JID)

	if w.groups.get(g.ID).Followed() {
		t.Fatalf("a refresh that started before the unfollow followed the group again")
	}
}

func TestAChangeOfPrimaryNumberKeepsTheMemberSync(t *testing.T) {
	w := newWorld()
	g := w.followed(t, "120363000000000001@g.us", participants("5511981124410"), "n2")
	synced := w.groups.get(g.ID).MembersSyncedAt
	w.seats.put(&uw.Group{WorkspaceID: ws, InstanceID: "n1", JID: g.JID, Subject: "Moradores", WeCanSend: true, WeAreAdmin: true})

	if _, err := w.svc.Refresh(context.Background(), everyone(), g.ID); err != nil {
		t.Fatal(err)
	}
	stored := w.groups.get(g.ID)
	if stored.MembersSyncedAt == nil || synced == nil || stored.MemberCount != 1 {
		t.Fatalf("after a primary change synced=%v count=%d, want the sync kept and our numbers left out", stored.MembersSyncedAt, stored.MemberCount)
	}
}

func TestManagingMembersGoesThroughAnAdminNumberOfTheCaller(t *testing.T) {
	w := newWorld()
	g := w.followed(t, "120363000000000001@g.us", participants("5511981124410"), "n1")
	w.seats.put(&uw.Group{WorkspaceID: ws, InstanceID: "n1", JID: g.JID, Subject: "Moradores", WeCanSend: true, WeAreAdmin: true})

	if _, err := w.svc.ManageMembers(context.Background(), everyone(), g.ID, uw.GroupActionPromote, []string{"5511981124410"}); err != nil {
		t.Fatal(err)
	}
	if len(w.seatSync.participantCalls) != 1 || w.seatSync.participantCalls[0] != "n1|promote|5511981124410" {
		t.Fatalf("calls = %v, want one promotion through n1", w.seatSync.participantCalls)
	}
}

func TestManagingMembersNeedsAnAdminNumberAndAValidAction(t *testing.T) {
	w := newWorld()
	g := w.followed(t, "120363000000000001@g.us", participants("5511981124410"), "n1")

	if _, err := w.svc.ManageMembers(context.Background(), everyone(), g.ID, uw.GroupActionAdd, []string{"5511999990000"}); !errors.Is(err, uw.ErrNotGroupAdmin) {
		t.Fatalf("without an admin number = %v, want ErrNotGroupAdmin", err)
	}
	w.seats.put(&uw.Group{WorkspaceID: ws, InstanceID: "n1", JID: g.JID, Subject: "Moradores", WeAreAdmin: true})
	if _, err := w.svc.ManageMembers(context.Background(), everyone(), g.ID, uw.GroupActionApprove, []string{"5511999990000"}); !errors.Is(err, uw.ErrInvalidGroupAction) {
		t.Fatalf("approve = %v, want refused", err)
	}
	if _, err := w.svc.ManageMembers(context.Background(), everyone(), g.ID, uw.GroupActionAdd, nil); !errors.Is(err, uw.ErrNoParticipants) {
		t.Fatalf("empty add = %v, want ErrNoParticipants", err)
	}
	if len(w.seatSync.participantCalls) != 0 {
		t.Fatalf("a refused change reached WhatsApp: %v", w.seatSync.participantCalls)
	}
}

func TestAGroupOurNumberOwnsIsRecordedAsCreatedHere(t *testing.T) {
	w := newWorld()
	w.api.listed["n1"] = []*uw.Group{{JID: "120363000000000009@g.us", Subject: "Do celular", OwnerJID: "5511970001201@s.whatsapp.net"}}

	if _, err := w.svc.Discover(context.Background(), everyone()); err != nil {
		t.Fatal(err)
	}
	g, err := w.groups.FindByJID(context.Background(), ws, "120363000000000009@g.us")
	if err != nil || g.Origin != uwg.OriginManaged {
		t.Fatalf("group = %+v, %v, want a group our number owns recorded as created here", g, err)
	}
}
