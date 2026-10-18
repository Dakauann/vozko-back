package stage_repository

import (
	"testing"

	"github.com/google/uuid"

	"vozko/domain/stage"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func TestAStageGroupReadsAndDeletesItsOwnItems(t *testing.T) {
	db := repotest.IsolatedDB(t, "stage_group", &schema.StageGroup{}, &schema.StageGroupItem{})
	repo := NewStageGroupRepository(db)
	group := &stage.StageGroup{WorkspaceID: uuid.NewString(), Name: "Vendas", Items: []stage.StageGroupItem{
		{Name: "novo", Position: 1}, {Name: "ganho", Position: 2},
	}}
	if err := repo.Create(group); err != nil {
		t.Fatal(err)
	}

	items, err := repo.GetItems(group.ID)
	if err != nil || len(items) != 2 || items[0].Name != "novo" {
		t.Fatalf("items = %+v, %v; want the two items in position order", items, err)
	}
	if err := repo.Delete(group.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	var left int64
	db.Model(&schema.StageGroupItem{}).Where("stage_group_id = ?", group.ID).Count(&left)
	if left != 0 {
		t.Fatalf("%d items left behind, want the group's items deleted with it", left)
	}
}
