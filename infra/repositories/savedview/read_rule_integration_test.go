package savedview_repository

import (
	"sort"
	"strings"
	"testing"

	"github.com/google/uuid"

	"vozko/domain/savedview"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func TestListForUserMatchesTheSharedReadRuleAgainstPostgres(t *testing.T) {
	db := repotest.IsolatedDB(t, "saved_view_read", &schema.SavedView{})
	repo := NewRepository(db)
	ws := uuid.NewString()
	ownerA, ownerB, outsider := uuid.NewString(), uuid.NewString(), uuid.NewString()

	var seeded []*savedview.SavedView
	for _, owner := range []string{ownerA, ownerB} {
		for _, visibility := range []savedview.Visibility{savedview.VisibilityPrivate, savedview.VisibilityShared} {
			v := &savedview.SavedView{
				ID: uuid.NewString(), WorkspaceID: ws, OwnerID: owner, ObjectType: savedview.ObjectLead,
				Name: string(visibility), GroupBy: savedview.GroupByNone, Visibility: visibility,
			}
			if err := repo.Create(v); err != nil {
				t.Fatalf("seed: %v", err)
			}
			seeded = append(seeded, v)
		}
	}

	for _, viewer := range []string{ownerA, ownerB, outsider} {
		listed, err := repo.ListForUser(ws, viewer, savedview.ObjectLead)
		if err != nil {
			t.Fatal(err)
		}
		var want []string
		for _, v := range seeded {
			if v.Owned().CanRead(viewer) {
				want = append(want, v.ID)
			}
		}
		got := make([]string, 0, len(listed))
		for _, v := range listed {
			got = append(got, v.ID)
		}
		sort.Strings(got)
		sort.Strings(want)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("viewer %s: the repository predicate drifted from Owned.CanRead:\n got %v\nwant %v", viewer, got, want)
		}
	}
}
