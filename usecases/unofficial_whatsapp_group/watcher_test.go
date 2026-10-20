package unofficial_whatsapp_group_usecase

import (
	"context"
	"testing"
	"time"

	uw "vozko/domain/unofficial_whatsapp"
	uwg "vozko/domain/unofficial_whatsapp_group"
)

func (w *world) instance(id string) *uw.Instance {
	i, _ := w.instances.FindByID(context.Background(), id)
	return i
}

func (w *world) followed(t *testing.T, jid string, roster []uw.GroupParticipant, instanceIDs ...string) *uwg.Group {
	t.Helper()
	g := w.groupWithSeats(t, jid, "Moradores", instanceIDs...)
	w.seatSync.rosters[jid] = roster
	if _, err := w.svc.Follow(context.Background(), everyone(), g.ID); err != nil {
		t.Fatal(err)
	}
	return w.groups.get(g.ID)
}

func (w *world) countAnalysis(t *testing.T, groupID string) *uwg.Analysis {
	t.Helper()
	a := &uwg.Analysis{WorkspaceID: ws, GroupID: groupID, Name: "Monitor", Prompt: "Fila", Status: uwg.AnalysisActive,
		Trigger: uwg.TriggerMessageCount, MessagesPerCycle: 50}
	if err := w.analyses.Create(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestASeatSyncInAWorkspaceThatNeverUsedGroupsWritesNothing(t *testing.T) {
	w := newWorld()
	seat := &uw.Group{WorkspaceID: ws, InstanceID: "n1", JID: "120363000000000001@g.us", Subject: "Moradores", Participants: participants("5511981124410")}

	w.svc.SeatSynced(context.Background(), w.instance("n1"), seat)

	if exists, _ := w.groups.Exists(context.Background(), ws); exists {
		t.Fatalf("a workspace without groups got a group row from a webhook")
	}
}

func TestASeatSyncInAWorkspaceWithGroupsRecordsTheNewGroup(t *testing.T) {
	w := newWorld()
	w.groupWithSeats(t, "120363000000000009@g.us", "Antigo", "n1")
	seat := &uw.Group{WorkspaceID: ws, InstanceID: "n2", JID: "120363000000000001@g.us", Subject: "Novo grupo"}
	w.seats.put(seat)

	w.svc.SeatSynced(context.Background(), w.instance("n2"), seat)

	g, err := w.groups.FindByJID(context.Background(), ws, seat.JID)
	if err != nil || g.Subject != "Novo grupo" || g.PrimaryInstanceID != "n2" {
		t.Fatalf("group = %+v, %v", g, err)
	}
}

func TestAPrimarySeatSyncOfAFollowedGroupUpdatesMembers(t *testing.T) {
	w := newWorld()
	g := w.followed(t, "120363000000000001@g.us", participants("5511981124410"), "n1")
	seat := &uw.Group{WorkspaceID: ws, InstanceID: "n1", JID: g.JID, Subject: "Moradores", Participants: participants("5511981124410", "5511960213375")}

	w.svc.SeatSynced(context.Background(), w.instance("n1"), seat)

	if w.members.eventsOf(uwg.MemberEventJoin) != 1 {
		t.Fatalf("joins = %d, want Camila's join recorded", w.members.eventsOf(uwg.MemberEventJoin))
	}
}

func TestOnlyTextFromThePrimaryNumberCountsTowardsAnalysis(t *testing.T) {
	w := newWorld()
	g := w.followed(t, "120363000000000001@g.us", participants("5511981124410"), "n1", "n2")
	a := w.countAnalysis(t, g.ID)
	ctx := context.Background()

	w.svc.GroupMessage(ctx, w.instance("n1"), g.JID, true)
	w.svc.GroupMessage(ctx, w.instance("n1"), g.JID, false)
	w.svc.GroupMessage(ctx, w.instance("n2"), g.JID, true)

	if got := w.analyses.get(a.ID).PendingMessages; got != 1 {
		t.Fatalf("pending = %d, want one text message from the primary number", got)
	}
}

func TestMessagesOfAGroupThatIsNotFollowedCountNothing(t *testing.T) {
	w := newWorld()
	g := w.groupWithSeats(t, "120363000000000001@g.us", "Moradores", "n1")
	a := w.countAnalysis(t, g.ID)

	w.svc.GroupMessage(context.Background(), w.instance("n1"), g.JID, true)

	if got := w.analyses.get(a.ID).PendingMessages; got != 0 {
		t.Fatalf("pending = %d for an unfollowed group", got)
	}
}

func TestWhenThePrimaryNumberDisconnectsTheNextNumberTakesOver(t *testing.T) {
	w := newWorld()
	g := w.followed(t, "120363000000000001@g.us", participants("5511981124410"), "n1", "n2")
	a := w.countAnalysis(t, g.ID)
	w.instance("n1").Status = uw.StatusDisconnected

	w.svc.GroupMessage(context.Background(), w.instance("n2"), g.JID, true)

	if w.groups.get(g.ID).PrimaryInstanceID != "n2" || w.analyses.get(a.ID).PendingMessages != 1 {
		t.Fatalf("primary = %s pending = %d, want n2 promoted and its message counted",
			w.groups.get(g.ID).PrimaryInstanceID, w.analyses.get(a.ID).PendingMessages)
	}
}

func TestThePrimaryIsRecheckedAtMostOncePerMinute(t *testing.T) {
	w := newWorld()
	g := w.followed(t, "120363000000000001@g.us", participants("5511981124410"), "n1", "n2")
	saves := w.groups.saves

	w.svc.GroupMessage(context.Background(), w.instance("n2"), g.JID, true)
	w.svc.GroupMessage(context.Background(), w.instance("n2"), g.JID, true)

	if w.svc.primaryCheckDue(g.ID, testNow.Add(30*time.Second)) {
		t.Fatalf("a second recheck was allowed within the minute")
	}
	if w.groups.saves != saves {
		t.Fatalf("a still-valid primary was rewritten")
	}
}

func TestAGroupChangeResyncsFollowedGroupsOnly(t *testing.T) {
	w := newWorld()
	g := w.followed(t, "120363000000000001@g.us", participants("5511981124410"), "n1")
	other := w.groupWithSeats(t, "120363000000000002@g.us", "Outro", "n1")
	before := len(w.seatSync.refreshs)

	w.seatSync.rosters[g.JID] = participants("5511981124410", "5511960213375")
	w.svc.GroupChanged(context.Background(), w.instance("n1"), g.JID, uw.GroupParticipantChange{})
	w.svc.GroupChanged(context.Background(), w.instance("n1"), other.JID, uw.GroupParticipantChange{})

	if len(w.seatSync.refreshs) != before+1 {
		t.Fatalf("refreshes = %v, want only the followed group resynced", w.seatSync.refreshs[before:])
	}
	if w.members.eventsOf(uwg.MemberEventJoin) != 1 {
		t.Fatalf("the change did not reach the members")
	}
}

func (f *fakeMembers) statusOf(groupID, personKey string) uwg.MemberStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.byGroup[groupID][personKey].Status
}

func (f *fakeMembers) leaveCauses() []uwg.DepartureCause {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []uwg.DepartureCause
	for _, e := range f.events {
		if e.Kind == uwg.MemberEventLeave {
			out = append(out, e.Cause)
		}
	}
	return out
}

func TestAWebhookRemovalByAnotherMemberIsRecordedAsRemoved(t *testing.T) {
	w := newWorld()
	g := w.followed(t, "120363000000000001@g.us", participants("5511981124410", "5511960213375"), "n1")

	w.seatSync.rosters[g.JID] = participants("5511981124410")
	w.svc.GroupChanged(context.Background(), w.instance("n1"), g.JID, uw.GroupParticipantChange{
		Actors: []string{"5511981124410@s.whatsapp.net"}, Left: []string{"5511960213375@s.whatsapp.net"},
	})

	if got := w.members.statusOf(g.ID, "5511960213375"); got != uwg.MemberRemoved {
		t.Fatalf("status = %q, want removed", got)
	}
	if causes := w.members.leaveCauses(); len(causes) != 1 || causes[0] != uwg.CauseRemoved {
		t.Fatalf("leave causes = %v, want one removal still counted as a leave", causes)
	}
}

func TestAWebhookDepartureWithoutAnActorStaysALeave(t *testing.T) {
	w := newWorld()
	g := w.followed(t, "120363000000000001@g.us", participants("5511981124410", "5511960213375"), "n1")

	w.seatSync.rosters[g.JID] = participants("5511981124410")
	w.svc.GroupChanged(context.Background(), w.instance("n1"), g.JID, uw.GroupParticipantChange{Left: []string{"5511960213375@s.whatsapp.net"}})

	if got := w.members.statusOf(g.ID, "5511960213375"); got != uwg.MemberLeft {
		t.Fatalf("status = %q, want left", got)
	}
}
