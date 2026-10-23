package lead

import (
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/lead"
	"vozko/domain/selection"
	"vozko/domain/shared"
)

func TestIncludedLeadsJoinTheFilterInsideTheWorkspaceAndStillLoseToExclusions(t *testing.T) {
	r := &repository{}
	s, err := r.selection(lead.SelectionQuery{WorkspaceID: pageWorkspace, Filter: blockedSelection(), IncludeIDs: []string{secondLead}, ExcludeIDs: []string{firstLead}, Require: notBlocked()})
	if err != nil {
		t.Fatal(err)
	}
	sql, args := s.countSQL()
	want := "SELECT COUNT(*) FROM leads WHERE ((leads.workspace_id = ? AND leads.deleted_at IS NULL AND ((leads.blocked = true))) OR (" +
		"leads.workspace_id = ? AND leads.deleted_at IS NULL AND leads.id = ANY(?::uuid[]))) AND NOT (leads.id = ANY(?::uuid[])) AND ((leads.blocked = false))"
	if sql != want {
		t.Fatalf("count SQL\n got: %s\nwant: %s", sql, want)
	}
	if args[0] != pageWorkspace || args[1] != pageWorkspace {
		t.Fatalf("the included leads are not bound to the workspace: %v", args)
	}
}

func TestFirstNTakesItsHeadThenAddsTheIncludedLeads(t *testing.T) {
	r := &repository{}
	s, err := r.selection(lead.SelectionQuery{
		WorkspaceID: pageWorkspace, Filter: blockedSelection(), Order: []shared.Sort{{Field: string(lead.SortName), Direction: shared.SortAsc}}, Limit: 3,
		IncludeIDs: []string{secondLead}, ExcludeIDs: []string{firstLead},
	})
	if err != nil {
		t.Fatal(err)
	}
	head := "(SELECT leads.id FROM leads WHERE leads.workspace_id = ? AND leads.deleted_at IS NULL AND ((leads.blocked = true)) AND NOT (leads.id = ANY(?::uuid[])) ORDER BY NULLIF(leads.name, '') ASC NULLS LAST, leads.id DESC LIMIT ?)" +
		" UNION SELECT leads.id FROM leads WHERE leads.workspace_id = ? AND leads.deleted_at IS NULL AND leads.id = ANY(?::uuid[]) AND NOT (leads.id = ANY(?::uuid[]))"
	if sql, _ := s.countSQL(); sql != "SELECT COUNT(*) FROM ("+head+") counted" {
		t.Fatalf("count SQL: %s", sql)
	}
	if sql, args := s.pageSQL("", 20); !strings.HasPrefix(sql, "SELECT chosen.id FROM ("+head+") chosen") || args[len(args)-1] != 20 {
		t.Fatalf("page SQL: %s", sql)
	}
	if sql, _ := s.freezeSQL(snapshotID, time.Unix(0, 0)); !strings.Contains(sql, "FROM leads WHERE leads.id IN ("+head+") ON CONFLICT DO NOTHING") {
		t.Fatalf("freeze SQL: %s", sql)
	}
}

func TestPickedIDsRefuseIncludedLeads(t *testing.T) {
	r := &repository{}
	if _, err := r.selection(lead.SelectionQuery{WorkspaceID: pageWorkspace, IDs: []string{firstLead}, IncludeIDs: []string{secondLead}}); !errors.Is(err, selection.ErrAmbiguousSelection) {
		t.Fatalf("picked ids with included leads = %v", err)
	}
}
