package unofficial_whatsapp_group

import (
	"testing"
	"time"

	uw "vozko/domain/unofficial_whatsapp"
)

var diffNow = time.Date(2026, 10, 17, 15, 0, 0, 0, time.UTC)

func participant(jid, phone, name string, role uw.GroupRole) uw.GroupParticipant {
	return uw.GroupParticipant{JID: jid, PhoneNumber: phone, DisplayName: name, Role: role}
}

func activeMember(key, name string) Member {
	joined := diffNow.Add(-48 * time.Hour)
	return Member{GroupID: "g1", WorkspaceID: "w1", PersonKey: key, JID: key + "@s.whatsapp.net", Phone: key, DisplayName: name,
		Role: uw.GroupRoleMember, Status: MemberActive, JoinedAt: &joined}
}

func eventsOf(d MemberDiff, kind MemberEventKind) []MemberEvent {
	var out []MemberEvent
	for _, e := range d.Events {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

func TestPersonKeyPrefersThePhoneAndFallsBackToTheBareJID(t *testing.T) {
	cases := []struct{ phone, jid, want string }{
		{"5511981124410", "5511981124410:12@s.whatsapp.net", "5511981124410"},
		{"", "5511981124410:12@s.whatsapp.net", "5511981124410@s.whatsapp.net"},
		{"", "161847220033@lid", "161847220033@lid"},
		{"+55 (11) 98112-4410", "", "5511981124410"},
		{"", "", ""},
	}
	for _, c := range cases {
		if got := PersonKey(c.phone, c.jid); got != c.want {
			t.Fatalf("PersonKey(%q, %q) = %q, want %q", c.phone, c.jid, got, c.want)
		}
	}
}

func TestFirstRosterOfAFollowedGroupIsTheBaselineAndCountsNoJoins(t *testing.T) {
	roster := []uw.GroupParticipant{
		participant("5511981124410@s.whatsapp.net", "5511981124410", "Rita", uw.GroupRoleMember),
		participant("5511992307781@s.whatsapp.net", "5511992307781", "Jorge", uw.GroupRoleAdmin),
	}
	d := DiffMembers("g1", "w1", nil, roster, OwnNumbers{}, diffNow, true)

	if len(d.Upserts) != 2 {
		t.Fatalf("baseline upserts = %d, want 2", len(d.Upserts))
	}
	if len(d.Events) != 0 {
		t.Fatalf("baseline events = %d, want none: the first roster is not growth", len(d.Events))
	}
	for _, m := range d.Upserts {
		if m.Status != MemberActive || m.JoinedAt != nil {
			t.Fatalf("baseline member %s = status %s joined %v, want active with no join time", m.PersonKey, m.Status, m.JoinedAt)
		}
	}
}

func TestNewParticipantAfterTheBaselineIsAJoin(t *testing.T) {
	current := []Member{activeMember("5511981124410", "Rita")}
	roster := []uw.GroupParticipant{
		participant("5511981124410@s.whatsapp.net", "5511981124410", "Rita", uw.GroupRoleMember),
		participant("5511960213375@s.whatsapp.net", "5511960213375", "Camila", uw.GroupRoleMember),
	}
	d := DiffMembers("g1", "w1", current, roster, OwnNumbers{}, diffNow, false)

	joins := eventsOf(d, MemberEventJoin)
	if len(joins) != 1 || joins[0].PersonKey != "5511960213375" {
		t.Fatalf("joins = %+v, want only Camila", joins)
	}
	if len(d.Upserts) != 1 || d.Upserts[0].JoinedAt == nil || !d.Upserts[0].JoinedAt.Equal(diffNow) {
		t.Fatalf("upserts = %+v, want Camila joined now", d.Upserts)
	}
}

func TestMissingActiveMemberLeavesAndIsKept(t *testing.T) {
	current := []Member{activeMember("5511981124410", "Rita"), activeMember("5511974550192", "Paula")}
	roster := []uw.GroupParticipant{
		participant("5511981124410@s.whatsapp.net", "5511981124410", "Rita", uw.GroupRoleMember),
	}
	d := DiffMembers("g1", "w1", current, roster, OwnNumbers{}, diffNow, false)

	leaves := eventsOf(d, MemberEventLeave)
	if len(leaves) != 1 || leaves[0].PersonKey != "5511974550192" {
		t.Fatalf("leaves = %+v, want only Paula", leaves)
	}
	if len(d.Upserts) != 1 {
		t.Fatalf("upserts = %d, want Paula only", len(d.Upserts))
	}
	left := d.Upserts[0]
	if left.Status != MemberLeft || left.LeftAt == nil || !left.LeftAt.Equal(diffNow) {
		t.Fatalf("Paula = %+v, want left now and kept as history", left)
	}
}

func TestReturningMemberJoinsAgain(t *testing.T) {
	left := activeMember("5511974550192", "Paula")
	leftAt := diffNow.Add(-24 * time.Hour)
	left.Status, left.LeftAt = MemberLeft, &leftAt
	roster := []uw.GroupParticipant{participant("5511974550192@s.whatsapp.net", "5511974550192", "Paula", uw.GroupRoleMember)}

	d := DiffMembers("g1", "w1", []Member{left}, roster, OwnNumbers{}, diffNow, false)

	if joins := eventsOf(d, MemberEventJoin); len(joins) != 1 {
		t.Fatalf("joins = %d, want 1 for the return", len(joins))
	}
	back := d.Upserts[0]
	if back.Status != MemberActive || back.LeftAt != nil || !back.JoinedAt.Equal(diffNow) {
		t.Fatalf("returning member = %+v, want active again with a fresh join", back)
	}
}

func TestUnchangedRosterWritesNothing(t *testing.T) {
	current := []Member{activeMember("5511981124410", "Rita")}
	roster := []uw.GroupParticipant{participant("5511981124410@s.whatsapp.net", "5511981124410", "Rita", uw.GroupRoleMember)}

	d := DiffMembers("g1", "w1", current, roster, OwnNumbers{}, diffNow, false)

	if !d.Empty() {
		t.Fatalf("diff = %+v, want nothing to write for an unchanged roster", d)
	}
}

func TestNameOrRoleChangeUpdatesWithoutAnEvent(t *testing.T) {
	current := []Member{activeMember("5511981124410", "Rita")}
	roster := []uw.GroupParticipant{participant("5511981124410@s.whatsapp.net", "5511981124410", "Rita Camargo", uw.GroupRoleAdmin)}

	d := DiffMembers("g1", "w1", current, roster, OwnNumbers{}, diffNow, false)

	if len(d.Events) != 0 {
		t.Fatalf("events = %+v, want none for a rename or promotion", d.Events)
	}
	if len(d.Upserts) != 1 || d.Upserts[0].DisplayName != "Rita Camargo" || d.Upserts[0].Role != uw.GroupRoleAdmin {
		t.Fatalf("upserts = %+v, want the new name and role", d.Upserts)
	}
	if !d.Upserts[0].JoinedAt.Equal(*current[0].JoinedAt) {
		t.Fatalf("join time changed on a rename")
	}
}

func TestOwnNumbersAreFlaggedSoGrowthCanLeaveThemOut(t *testing.T) {
	own := NewOwnNumbers([]string{"5511970001201"}, nil)
	roster := []uw.GroupParticipant{participant("5511970001201:3@s.whatsapp.net", "5511970001201", "Vendas 1", uw.GroupRoleMember)}

	d := DiffMembers("g1", "w1", []Member{activeMember("5511981124410", "Rita")}, roster, own, diffNow, false)

	for _, m := range d.Upserts {
		if m.PersonKey == "5511970001201" && !m.OurNumber {
			t.Fatalf("our number was not flagged")
		}
	}
	for _, e := range d.Events {
		if e.PersonKey == "5511970001201" && !e.OurNumber {
			t.Fatalf("our number's join event was not flagged")
		}
		if e.PersonKey == "5511981124410" && e.OurNumber {
			t.Fatalf("a member was flagged as our number")
		}
	}
}

func TestParticipantWithoutIdentityIsIgnored(t *testing.T) {
	roster := []uw.GroupParticipant{participant("", "", "Ghost", uw.GroupRoleMember)}
	d := DiffMembers("g1", "w1", nil, roster, OwnNumbers{}, diffNow, false)
	if !d.Empty() {
		t.Fatalf("diff = %+v, want an identity-less participant ignored", d)
	}
}

func TestEmptyRosterNeverEmptiesTheGroup(t *testing.T) {
	current := []Member{activeMember("5511981124410", "Rita")}
	d := DiffMembers("g1", "w1", current, nil, OwnNumbers{}, diffNow, false)
	if !d.Empty() {
		t.Fatalf("diff = %+v, want an empty roster treated as unknown, not as everyone leaving", d)
	}
}

func TestLeadNumbersCoverBothBrazilianMobileForms(t *testing.T) {
	m := Member{Phone: "551181124410"}
	got := m.LeadNumbers()
	want := map[string]bool{"551181124410": true, "5511981124410": true}
	if len(got) != 2 || !want[got[0]] || !want[got[1]] {
		t.Fatalf("LeadNumbers = %v, want both the 12 and 13 digit forms", got)
	}
	if n := (Member{}).LeadNumbers(); len(n) != 0 {
		t.Fatalf("hidden member has lead numbers %v", n)
	}
}

func TestABaselineAfterUnfollowingRecordsNoEvents(t *testing.T) {
	current := []Member{activeMember("5511981124410", "Rita"), activeMember("5511974550192", "Paula")}
	roster := []uw.GroupParticipant{
		participant("5511981124410@s.whatsapp.net", "5511981124410", "Rita", uw.GroupRoleMember),
		participant("5511960213375@s.whatsapp.net", "5511960213375", "Camila", uw.GroupRoleMember),
	}
	d := DiffMembers("g1", "w1", current, roster, OwnNumbers{}, diffNow, true)
	if len(d.Events) != 0 {
		t.Fatalf("events = %+v, want a new baseline with no growth recorded", d.Events)
	}
	if len(d.Upserts) != 2 {
		t.Fatalf("upserts = %+v, want Camila added and Paula marked as gone", d.Upserts)
	}
}

func TestMembershipsGroupByPersonAndShareOneLead(t *testing.T) {
	rita := activeMember("5511981124410", "Rita")
	ritaElsewhere := activeMember("5511981124410", "")
	ritaElsewhere.GroupID, ritaElsewhere.LeadID = "g2", "lead-1"
	hidden := Member{GroupID: "g1", PersonKey: "161847220033@lid", JID: "161847220033@lid", Status: MemberActive}

	people := MembershipsByPerson([]Member{rita, hidden, ritaElsewhere})

	if len(people) != 2 || people[0].PersonKey() != "5511981124410" || people[1].PersonKey() != "161847220033@lid" {
		t.Fatalf("people = %+v, want Rita then the hidden member", people)
	}
	if people[0].LeadID() != "lead-1" || people[0].Phone() != "5511981124410" || people[0].Name() != "Rita" {
		t.Fatalf("Rita = lead %q phone %q name %q", people[0].LeadID(), people[0].Phone(), people[0].Name())
	}
	if unlinked := people[0].Unlinked(); len(unlinked) != 1 || unlinked[0].GroupID != "g1" {
		t.Fatalf("unlinked = %+v, want only the g1 membership", unlinked)
	}
	if people[1].Phone() != "" {
		t.Fatalf("a hidden member has no phone to make a lead from")
	}
}
