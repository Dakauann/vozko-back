package unofficial_whatsapp_group_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vozko/domain/report"
	uw "vozko/domain/unofficial_whatsapp"
	uwg "vozko/domain/unofficial_whatsapp_group"
)

func TestMembersOfAGroupOutsideTheScopeAreNotListed(t *testing.T) {
	w := newWorld()
	g := w.groupWithSeats(t, "120363000000000002@g.us", "Ofertas", "n2")
	if _, err := w.svc.ListMembers(context.Background(), salesOnly(), g.ID, uwg.MemberFilter{}); !errors.Is(err, uwg.ErrGroupNotFound) {
		t.Fatalf("ListMembers = %v, want not found", err)
	}
}

func TestAddingMembersToLeadsSkipsHiddenAndAlreadyLinkedPeople(t *testing.T) {
	w := newWorld()
	roster := append(participants("5511981124410", "5511960213375"), uw.GroupParticipant{JID: "161847220033@lid", LID: "161847220033@lid", DisplayName: "Antônio"})
	g := w.followed(t, "120363000000000001@g.us", roster, "n1")
	_ = w.members.SetLead(context.Background(), g.ID, "5511960213375", "lead-existing")

	result, err := w.svc.AddMembersToLeads(context.Background(), everyone(), g.ID, []string{"5511981124410", "5511960213375", "161847220033@lid"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Linked != 1 || result.AlreadyLinked != 1 || result.WithoutPhone != 1 {
		t.Fatalf("result = %+v", result)
	}
	if len(w.leads.linked) != 1 || w.leads.linked[0] != "5511981124410" {
		t.Fatalf("leads created for %v, want only Rita", w.leads.linked)
	}
	members, _ := w.members.ByPersonKeys(context.Background(), g.ID, []string{"5511981124410"})
	if members[0].LeadID != "lead-5511981124410" {
		t.Fatalf("member lead = %q", members[0].LeadID)
	}
}

func TestExportWritesOneRowPerMemberWithAHeader(t *testing.T) {
	w := newWorld()
	roster := participants("5511981124410", "5511960213375")
	roster[1].DisplayName = "=HYPERLINK(\"x\")"
	g := w.followed(t, "120363000000000001@g.us", roster, "n1")

	export, err := w.svc.ExportMembers(context.Background(), everyone(), g.ID)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(strings.TrimPrefix(export.Document, report.UTF8BOM), report.CSVNewline), report.CSVNewline)
	if !strings.HasPrefix(export.Document, report.UTF8BOM) || len(lines) != 3 || !strings.HasPrefix(lines[0], "Grupo;JID do grupo") {
		t.Fatalf("document = %q, want a header and two members", export.Document)
	}
	if strings.Contains(export.Document, ";=HYPERLINK") || strings.HasPrefix(lines[2], "=") {
		t.Fatalf("a member name reached the sheet as a formula: %q", export.Document)
	}
	if export.FileName == "" {
		t.Fatalf("export has no file name")
	}
}

func TestLinkingAMemberToALeadOnlyTouchesThatGroup(t *testing.T) {
	w := newWorld()
	first := w.followed(t, "120363000000000001@g.us", participants("5511981124410"), "n1")
	second := w.followed(t, "120363000000000002@g.us", participants("5511981124410"), "n2")

	if _, err := w.svc.AddMembersToLeads(context.Background(), salesOnly(), first.ID, []string{"5511981124410"}); err != nil {
		t.Fatal(err)
	}
	other, _ := w.members.ByPersonKeys(context.Background(), second.ID, []string{"5511981124410"})
	if other[0].LeadID != "" {
		t.Fatalf("a member of another department's group was linked to the lead")
	}
}

func TestAddingPeopleToLeadsLinksEveryVisibleMembership(t *testing.T) {
	w := newWorld()
	roster := append(participants("5511981124410"), uw.GroupParticipant{JID: "161847220033@lid", LID: "161847220033@lid", DisplayName: "Antônio"})
	first := w.followed(t, "120363000000000001@g.us", roster, "n1")
	second := w.followed(t, "120363000000000002@g.us", participants("5511981124410", "5511960213375"), "n1")
	_ = w.members.SetLead(context.Background(), second.ID, "5511960213375", "lead-existing")

	result, err := w.svc.AddPeopleToLeads(context.Background(), everyone(), []string{"5511981124410", "5511960213375", "161847220033@lid"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Linked != 1 || result.AlreadyLinked != 1 || result.WithoutPhone != 1 {
		t.Fatalf("result = %+v", result)
	}
	if len(w.leads.linked) != 1 {
		t.Fatalf("leads created for %v, want one lead for Rita", w.leads.linked)
	}
	for _, g := range []*uwg.Group{first, second} {
		members, _ := w.members.ByPersonKeys(context.Background(), g.ID, []string{"5511981124410"})
		if members[0].LeadID != "lead-5511981124410" {
			t.Fatalf("Rita in %s has lead %q, want the same lead in every group", g.Subject, members[0].LeadID)
		}
	}
}

func TestAddingPeopleToLeadsStaysInsideTheScope(t *testing.T) {
	w := newWorld()
	w.followed(t, "120363000000000001@g.us", participants("5511981124410"), "n1")
	theirs := w.followed(t, "120363000000000002@g.us", participants("5511981124410"), "n2")

	if _, err := w.svc.AddPeopleToLeads(context.Background(), salesOnly(), []string{"5511981124410"}); err != nil {
		t.Fatal(err)
	}
	other, _ := w.members.ByPersonKeys(context.Background(), theirs.ID, []string{"5511981124410"})
	if other[0].LeadID != "" {
		t.Fatalf("a membership in another department's group was linked")
	}
}
