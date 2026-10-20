package unofficial_whatsapp_group_usecase

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/report"
	uw "vozko/domain/unofficial_whatsapp"
	uwg "vozko/domain/unofficial_whatsapp_group"
)

func exportLines(t *testing.T, export *MemberExport) [][]string {
	t.Helper()
	if !strings.HasPrefix(export.Document, report.UTF8BOM) {
		t.Fatalf("document without BOM: %q", export.Document)
	}
	var out [][]string
	for _, line := range strings.Split(strings.TrimSuffix(strings.TrimPrefix(export.Document, report.UTF8BOM), report.CSVNewline), report.CSVNewline) {
		out = append(out, strings.Split(line, report.CSVDelimiter))
	}
	return out
}

func column(t *testing.T, header []string, name string) int {
	t.Helper()
	for i, h := range header {
		if h == name {
			return i
		}
	}
	t.Fatalf("header %v has no column %q", header, name)
	return -1
}

func TestSeveralGroupsExportIntoOneFile(t *testing.T) {
	w := newWorld()
	w.instances.instances[0].DisplayName = "Vendas"
	first := w.followed(t, "120363000000000001@g.us", append(participants("5511981124410"), participants("5511970001201")...), "n1")
	second := w.followed(t, "120363000000000002@g.us", participants("5511960213375"), "n2")

	export, err := w.svc.ExportGroupMembers(context.Background(), everyone(), MemberExportInput{GroupIDs: []string{first.ID, second.ID}})
	if err != nil {
		t.Fatal(err)
	}
	lines := exportLines(t, export)
	if len(lines) != 4 {
		t.Fatalf("lines = %d, want a header and three members: %q", len(lines), export.Document)
	}
	header := lines[0]
	want := []string{"Grupo", "JID do grupo", "Nossos números no grupo", "Nome", "Telefone", "JID", "LID", "Status", "Papel", "Entrou em", "Saiu em", "Lead", "Outros grupos", "Seu número"}
	if strings.Join(header, "|") != strings.Join(want, "|") {
		t.Fatalf("header = %v, want %v", header, want)
	}
	groups := map[string]bool{}
	for _, row := range lines[1:] {
		groups[row[column(t, header, "JID do grupo")]] = true
		if row[column(t, header, "Telefone")] == "5511970001201" {
			if row[column(t, header, "Seu número")] != "Vendas" {
				t.Fatalf("our member row = %v, want the number's name", row)
			}
		}
		if row[column(t, header, "JID do grupo")] == first.JID && row[column(t, header, "Nossos números no grupo")] != "Vendas" {
			t.Fatalf("first group row = %v, want our number named", row)
		}
	}
	if !groups[first.JID] || !groups[second.JID] {
		t.Fatalf("groups in the file = %v, want both", groups)
	}
	if !strings.Contains(export.FileName, "2") {
		t.Fatalf("file name = %q, want it to say how many groups", export.FileName)
	}
}

func TestTheExportSpellsOutStatusRoleAndHiddenNumbersInTheWorkspaceTime(t *testing.T) {
	w := newWorld()
	g := w.followed(t, "120363000000000001@g.us", participants("5511981124410"), "n1")
	joined := time.Date(2026, 10, 10, 13, 5, 0, 0, time.UTC)
	left := time.Date(2026, 10, 12, 2, 30, 0, 0, time.UTC)
	w.members.byGroup[g.ID]["5511960213375"] = uwg.Member{GroupID: g.ID, WorkspaceID: ws, PersonKey: "5511960213375", JID: "5511960213375@s.whatsapp.net",
		Phone: "5511960213375", Role: uw.GroupRoleSuperAdmin, Status: uwg.MemberRemoved, JoinedAt: &joined, LeftAt: &left}
	w.members.byGroup[g.ID]["161847220033@lid"] = uwg.Member{GroupID: g.ID, WorkspaceID: ws, PersonKey: "161847220033@lid", JID: "161847220033@lid",
		LID: "161847220033@lid", Role: uw.GroupRoleAdmin, Status: uwg.MemberLeft}

	export, err := w.svc.ExportGroupMembers(context.Background(), everyone(), MemberExportInput{GroupIDs: []string{g.ID}})
	if err != nil {
		t.Fatal(err)
	}
	lines := exportLines(t, export)
	header := lines[0]
	byKey := map[string][]string{}
	for _, row := range lines[1:] {
		byKey[row[column(t, header, "JID")]] = row
	}
	removed := byKey["5511960213375@s.whatsapp.net"]
	if removed[column(t, header, "Status")] != "Removido" || removed[column(t, header, "Papel")] != "Superadmin" {
		t.Fatalf("removed row = %v", removed)
	}
	if removed[column(t, header, "Entrou em")] != "10/10/2026 10:05" || removed[column(t, header, "Saiu em")] != "11/10/2026 23:30" {
		t.Fatalf("dates = %v, want the São Paulo wall clock", removed)
	}
	hidden := byKey["161847220033@lid"]
	if hidden[column(t, header, "LID")] != "161847220033@lid" || hidden[column(t, header, "Telefone")] != "" || hidden[column(t, header, "Status")] != "Saiu" || hidden[column(t, header, "Papel")] != "Admin" {
		t.Fatalf("hidden row = %v", hidden)
	}
}

func TestAWholeSeriesExportsTogether(t *testing.T) {
	w := newWorld()
	first := w.followed(t, "120363000000000001@g.us", participants("5511981124410"), "n1")
	second := w.followed(t, "120363000000000002@g.us", participants("5511960213375"), "n1")
	for i, g := range []*uwg.Group{first, second} {
		stored := w.groups.get(g.ID)
		stored.SeriesID, stored.SeriesSequence = "series-1", i+1
	}
	w.followed(t, "120363000000000003@g.us", participants("5511974550192"), "n1")

	export, err := w.svc.ExportGroupMembers(context.Background(), everyone(), MemberExportInput{SeriesID: "series-1"})
	if err != nil {
		t.Fatal(err)
	}
	if lines := exportLines(t, export); len(lines) != 3 {
		t.Fatalf("lines = %d, want the two series groups only: %q", len(lines), export.Document)
	}
}

func TestTheExportRefusesGroupsOutsideTheScopeAndAnEmptyChoice(t *testing.T) {
	w := newWorld()
	mine := w.followed(t, "120363000000000001@g.us", participants("5511981124410"), "n1")
	theirs := w.followed(t, "120363000000000002@g.us", participants("5511960213375"), "n2")

	if _, err := w.svc.ExportGroupMembers(context.Background(), salesOnly(), MemberExportInput{GroupIDs: []string{mine.ID, theirs.ID}}); !errors.Is(err, uwg.ErrGroupNotFound) {
		t.Fatalf("export = %v, want not found for a group outside the scope", err)
	}
	if _, err := w.svc.ExportGroupMembers(context.Background(), everyone(), MemberExportInput{}); !errors.Is(err, uwg.ErrNoGroupsToExport) {
		t.Fatalf("export = %v, want a refusal without groups", err)
	}
	if _, err := w.svc.ExportGroupMembers(context.Background(), salesOnly(), MemberExportInput{SeriesID: "unknown"}); !errors.Is(err, uwg.ErrGroupNotFound) {
		t.Fatalf("export = %v, want not found for a series with no visible group", err)
	}
}
