package unofficial_whatsapp_group

import (
	"testing"

	uw "vozko/domain/unofficial_whatsapp"
)

func adminMember(key, lid, name string) Member {
	m := activeMember(key, name)
	m.LID, m.Role = lid, uw.GroupRoleAdmin
	return m
}

func TestALeaverNamedByAnotherMemberWasRemoved(t *testing.T) {
	current := []Member{adminMember("5511992307781", "4455@lid", "Jorge"), activeMember("5511974550192", "Paula")}
	change := uw.GroupParticipantChange{Actors: []string{"4455@lid"}, Left: []string{"5511974550192@s.whatsapp.net"}}

	causes := CausesFromChange(change, current)

	if causes["5511974550192"] != CauseRemoved {
		t.Fatalf("causes = %v, want Paula removed by Jorge", causes)
	}
}

func TestALeaverWhoIsTheActorLeftOnTheirOwn(t *testing.T) {
	paula := activeMember("5511974550192", "Paula")
	paula.LID = "7788@lid"
	change := uw.GroupParticipantChange{Actors: []string{"7788@lid", "5511974550192@s.whatsapp.net"}, Left: []string{"7788@lid"}}

	causes := CausesFromChange(change, []Member{paula})

	if causes["5511974550192"] != CauseLeft {
		t.Fatalf("causes = %v, want Paula left on her own", causes)
	}
}

func TestAnUnknownActorDecidesNothing(t *testing.T) {
	current := []Member{activeMember("5511974550192", "Paula")}
	cases := map[string]uw.GroupParticipantChange{
		"no actor":          {Left: []string{"5511974550192@s.whatsapp.net"}},
		"actor not in list": {Actors: []string{"9999@lid"}, Left: []string{"5511974550192@s.whatsapp.net"}},
		"leaver not known":  {Actors: []string{"5511974550192@s.whatsapp.net"}, Left: []string{"5511900000000@s.whatsapp.net"}},
	}
	for name, change := range cases {
		if causes := CausesFromChange(change, current); len(causes) != 0 {
			t.Errorf("%s: causes = %v, want none", name, causes)
		}
	}
}

func TestOurRemovalNamesTheMembersWeRemoved(t *testing.T) {
	current := []Member{activeMember("5511974550192", "Paula"), activeMember("5511981124410", "Rita")}

	causes := RemovedByUs([]string{"5511974550192", "5511900000000@s.whatsapp.net"}, current)

	if len(causes) != 1 || causes["5511974550192"] != CauseRemoved {
		t.Fatalf("causes = %v, want only Paula removed", causes)
	}
}

func TestAttributingADiffMarksRemovalsAndKeepsThemDepartures(t *testing.T) {
	current := []Member{activeMember("5511974550192", "Paula"), activeMember("5511981124410", "Rita"), activeMember("5511960213375", "Camila")}
	roster := []uw.GroupParticipant{participant("5511960213375@s.whatsapp.net", "5511960213375", "Camila", uw.GroupRoleMember)}
	d := DiffMembers("g1", "w1", current, roster, OwnNumbers{}, diffNow, false)

	d.Attribute(DepartureCauses{"5511974550192": CauseRemoved, "5511981124410": CauseLeft, "5511960213375": CauseRemoved})

	statuses := map[string]MemberStatus{}
	for _, m := range d.Upserts {
		statuses[m.PersonKey] = m.Status
	}
	if statuses["5511974550192"] != MemberRemoved || statuses["5511981124410"] != MemberLeft {
		t.Fatalf("statuses = %v, want Paula removed and Rita left", statuses)
	}
	if _, touched := statuses["5511960213375"]; touched {
		t.Fatalf("Camila is still in the group and must not be written")
	}
	causes := map[string]DepartureCause{}
	for _, e := range eventsOf(d, MemberEventLeave) {
		causes[e.PersonKey] = e.Cause
	}
	if len(causes) != 2 || causes["5511974550192"] != CauseRemoved || causes["5511981124410"] != CauseLeft {
		t.Fatalf("leave events = %v, want both still departures with their cause", causes)
	}
}

func TestADepartureWithoutACauseStaysAPlainLeave(t *testing.T) {
	current := []Member{activeMember("5511974550192", "Paula")}
	d := DiffMembers("g1", "w1", current, []uw.GroupParticipant{participant("5511981124410@s.whatsapp.net", "5511981124410", "Rita", uw.GroupRoleMember)}, OwnNumbers{}, diffNow, false)

	d.Attribute(nil)

	for _, m := range d.Upserts {
		if m.PersonKey == "5511974550192" && m.Status != MemberLeft {
			t.Fatalf("Paula = %s, want left", m.Status)
		}
	}
	for _, e := range eventsOf(d, MemberEventLeave) {
		if e.Cause != CauseUnknown {
			t.Fatalf("cause = %q, want unknown", e.Cause)
		}
	}
}

func TestRemovedIsAValidStatusAndADeparture(t *testing.T) {
	if !MemberRemoved.Valid() {
		t.Fatal("removed must be a valid member status")
	}
	if MemberActive.Departed() || !MemberLeft.Departed() || !MemberRemoved.Departed() {
		t.Fatal("left and removed are departures, active is not")
	}
}

func TestOnlyAnActiveMemberCanBeTheOneWhoRemoved(t *testing.T) {
	gone := adminMember("5511992307781", "4455@lid", "Jorge")
	gone.Status = MemberLeft
	current := []Member{gone, activeMember("5511974550192", "Paula")}
	change := uw.GroupParticipantChange{Actors: []string{"4455@lid"}, Left: []string{"5511974550192@s.whatsapp.net"}}

	if causes := CausesFromChange(change, current); len(causes) != 0 {
		t.Fatalf("causes = %v, want none from an actor who is no longer in the group", causes)
	}
}
