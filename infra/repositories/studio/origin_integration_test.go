package studio_repository

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"vozko/domain/studio"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func originProject(t *testing.T, ws, user, name string, origin studio.Origin) *studio.Project {
	t.Helper()
	p, err := studio.NewProject(ws, user, studio.KindImage, name, imageDocument(t, "#ffffff"))
	if err != nil {
		t.Fatal(err)
	}
	if err := p.SetOrigin(origin); err != nil {
		t.Fatal(err)
	}
	return p
}

func countProjects(t *testing.T, db *gorm.DB, ws string) int64 {
	t.Helper()
	var n int64
	if err := db.Model(&schema.StudioProject{}).Where("workspace_id = ?", ws).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func TestARepeatedChatCommandCreateReturnsTheSameProject(t *testing.T) {
	db := repotest.IsolatedDB(t, "studio_projects_origin", &schema.StudioProject{})
	repo := New(db)
	ctx := context.Background()
	ws, user := uuid.NewString(), uuid.NewString()
	origin := studio.Origin{ThreadID: uuid.NewString(), CommandID: uuid.NewString()}
	first := originProject(t, ws, user, "Primeira aba", origin)
	if err := repo.Create(ctx, first); err != nil {
		t.Fatal(err)
	}
	second := originProject(t, ws, user, "Segunda aba", origin)
	if err := repo.Create(ctx, second); err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || second.Name != "Primeira aba" || second.Version != 1 || second.Origin != origin {
		t.Fatalf("first %s second %+v", first.ID, second)
	}
	if n := countProjects(t, db, ws); n != 1 {
		t.Fatalf("%d projects for one command", n)
	}
}

func TestDifferentCommandsAndManualCreatesMakeDistinctProjects(t *testing.T) {
	db := repotest.IsolatedDB(t, "studio_projects_origin_distinct", &schema.StudioProject{})
	repo := New(db)
	ctx := context.Background()
	ws, user, thread := uuid.NewString(), uuid.NewString(), uuid.NewString()
	projects := []*studio.Project{
		originProject(t, ws, user, "Um", studio.Origin{ThreadID: thread, CommandID: uuid.NewString()}),
		originProject(t, ws, user, "Dois", studio.Origin{ThreadID: thread, CommandID: uuid.NewString()}),
		originProject(t, ws, user, "Manual", studio.Origin{}),
		originProject(t, ws, user, "Manual", studio.Origin{}),
	}
	seen := map[string]bool{}
	for _, p := range projects {
		if err := repo.Create(ctx, p); err != nil {
			t.Fatal(err)
		}
		seen[p.ID] = true
	}
	if len(seen) != len(projects) || countProjects(t, db, ws) != int64(len(projects)) {
		t.Fatalf("ids %v rows %d", seen, countProjects(t, db, ws))
	}
}

func TestTabsRacingOnOneChatCommandAllGetTheSameProject(t *testing.T) {
	db := repotest.IsolatedDB(t, "studio_projects_origin_race", &schema.StudioProject{})
	repo := New(db)
	ws, user := uuid.NewString(), uuid.NewString()
	origin := studio.Origin{ThreadID: uuid.NewString(), CommandID: uuid.NewString()}
	const tabs = 8
	ids := make([]string, tabs)
	errs := make([]error, tabs)
	var wg sync.WaitGroup
	for i := range tabs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p := originProject(t, ws, user, "Aba", origin)
			errs[i] = repo.Create(context.Background(), p)
			ids[i] = p.ID
		}()
	}
	wg.Wait()
	for i := range tabs {
		if errs[i] != nil || ids[i] != ids[0] {
			t.Fatalf("tab %d id %s err %v first %s", i, ids[i], errs[i], ids[0])
		}
	}
	if n := countProjects(t, db, ws); n != 1 {
		t.Fatalf("%d projects for one command", n)
	}
}

func TestAChatCommandProjectOfAnotherOwnerIsNeverHandedOut(t *testing.T) {
	db := repotest.IsolatedDB(t, "studio_projects_origin_owner", &schema.StudioProject{})
	repo := New(db)
	ctx := context.Background()
	ws, user := uuid.NewString(), uuid.NewString()
	origin := studio.Origin{ThreadID: uuid.NewString(), CommandID: uuid.NewString()}
	if err := repo.Create(ctx, originProject(t, ws, user, "Dono", origin)); err != nil {
		t.Fatal(err)
	}
	for name, p := range map[string]*studio.Project{
		"another workspace": originProject(t, uuid.NewString(), user, "Intruso", origin),
		"another member":    originProject(t, ws, uuid.NewString(), "Intruso", origin),
	} {
		err := repo.Create(ctx, p)
		if !errors.Is(err, studio.ErrOriginTaken) || p.ID != "" {
			t.Fatalf("%s: err %v id %q", name, err, p.ID)
		}
	}
}
