package lead

import (
	"context"
	"sort"
	"testing"

	"github.com/google/uuid"

	"vozko/domain/lead"
	"vozko/domain/shared"
)

func pagedSelection(t *testing.T, repo *repository, q lead.SelectionQuery) []string {
	t.Helper()
	var all []string
	after := ""
	for {
		page, err := repo.SelectionPage(context.Background(), q, after, 7)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			return all
		}
		all = append(all, page...)
		after = page[len(page)-1]
	}
}

func sameSet(t *testing.T, label string, got, want []string) {
	t.Helper()
	got, want = append([]string{}, got...), append([]string{}, want...)
	sort.Strings(got)
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("%s = %v, want %v", label, got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("%s = %v, want %v", label, got, want)
		}
	}
}

func TestIncludedLeadsJoinEverySelectionModeAgainstPostgres(t *testing.T) {
	db := selectionDB(t)
	repo := &repository{db: db, agg: newAggregateCache(nil)}
	ctx := context.Background()
	ws, other := uuid.NewString(), uuid.NewString()
	seedLeads(t, db, ws, 100)
	foreign := seedLeads(t, db, other, 3)[0]
	var blocked, open []string
	db.Raw("SELECT id::text FROM leads WHERE workspace_id = ? AND blocked ORDER BY id", ws).Scan(&blocked)
	db.Raw("SELECT id::text FROM leads WHERE workspace_id = ? AND NOT blocked ORDER BY id", ws).Scan(&open)
	if len(blocked) != 10 || len(open) != 90 {
		t.Fatalf("seeded %d blocked and %d open leads", len(blocked), len(open))
	}
	deleted := open[2]
	if err := db.Exec("UPDATE leads SET deleted_at = now() WHERE id = ?", deleted).Error; err != nil {
		t.Fatal(err)
	}
	included := []string{open[0], open[1], deleted, foreign, blocked[0]}

	filtered := lead.SelectionQuery{WorkspaceID: ws, Filter: blockedSelection(), IncludeIDs: included, ExcludeIDs: []string{open[1]}}
	if n, err := repo.CountSelection(ctx, filtered); err != nil || n != 11 {
		t.Fatalf("filtered count = %d, %v, want the ten blocked plus one included", n, err)
	}
	sameSet(t, "filtered members", pagedSelection(t, repo, filtered), append(append([]string{}, blocked...), open[0]))

	everyone := lead.SelectionQuery{WorkspaceID: ws, IncludeIDs: []string{foreign, open[0]}}
	if n, err := repo.CountSelection(ctx, everyone); err != nil || n != 99 {
		t.Fatalf("everyone count = %d, %v, want the 99 live leads of the workspace", n, err)
	}

	firstN := lead.SelectionQuery{
		WorkspaceID: ws, Filter: blockedSelection(), Order: []shared.Sort{{Field: string(lead.SortName), Direction: shared.SortAsc}}, Limit: 3,
		IncludeIDs: included, ExcludeIDs: []string{open[1]},
	}
	var head []string
	db.Raw("SELECT id::text FROM leads WHERE workspace_id = ? AND deleted_at IS NULL AND blocked AND id <> ? ORDER BY name, id DESC LIMIT 3", ws, open[1]).Scan(&head)
	want := append(append([]string{}, head...), open[0])
	if !containsID(head, blocked[0]) {
		want = append(want, blocked[0])
	}
	if n, err := repo.CountSelection(ctx, firstN); err != nil || n != len(want) {
		t.Fatalf("first n count = %d, %v, want %d", n, err, len(want))
	}
	sameSet(t, "first n members", pagedSelection(t, repo, firstN), want)
	frozen, err := repo.FreezeSelection(ctx, firstN, uuid.NewString())
	if err != nil || frozen.Size != len(want) {
		t.Fatalf("first n freeze = %+v, %v, want %d", frozen, err, len(want))
	}

	required := lead.SelectionQuery{WorkspaceID: ws, Filter: blockedSelection(), IncludeIDs: []string{open[0], blocked[0]}, Require: notBlocked()}
	if n, err := repo.CountSelection(ctx, required); err != nil || n != 1 {
		t.Fatalf("a server requirement left %d leads, %v, want only the open included lead", n, err)
	}
}

func containsID(ids []string, id string) bool {
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}
	return false
}
