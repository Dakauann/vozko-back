package studio_repository

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"vozko/domain/studio"
	"vozko/infra/database/schema"
	"vozko/infra/repositories/repotest"
)

func imageDocument(t *testing.T, background string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(studio.LegacyImageDocument{Schema: studio.SchemaImage, Version: studio.DocumentVersion,
		Canvas: studio.Canvas{Width: 1080, Height: 1080, Background: background}, Layers: []studio.Layer{}})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestAProjectIsSavedOnlyFromTheVersionItWasLoadedAt(t *testing.T) {
	db := repotest.IsolatedDB(t, "studio_projects_save", &schema.StudioProject{})
	repo := New(db)
	ctx := context.Background()
	ws := uuid.NewString()
	p, err := studio.NewProject(ws, uuid.NewString(), studio.KindImage, "Post", imageDocument(t, "#ffffff"))
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	tabA, _ := repo.Get(ctx, ws, p.ID)
	tabB, _ := repo.Get(ctx, ws, p.ID)
	tabA.Document = imageDocument(t, "#000000")
	if err := repo.Save(ctx, tabA, 1); err != nil || tabA.Version != 2 {
		t.Fatalf("first save %v version %d", err, tabA.Version)
	}
	tabB.Document = imageDocument(t, "#ff0000")
	if err := repo.Save(ctx, tabB, 1); !errors.Is(err, studio.ErrVersionConflict) {
		t.Fatalf("a stale tab overwrote the project: %v", err)
	}
	stored, _ := repo.Get(ctx, ws, p.ID)
	var doc studio.LegacyImageDocument
	_ = json.Unmarshal(stored.Document, &doc)
	if doc.Canvas.Background != "#000000" || stored.Version != 2 {
		t.Fatalf("stored %+v", doc.Canvas)
	}
	if _, err := repo.Get(ctx, uuid.NewString(), p.ID); !errors.Is(err, studio.ErrProjectNotFound) {
		t.Fatalf("another workspace read the project: %v", err)
	}
	ghost := *stored
	ghost.ID = uuid.NewString()
	if err := repo.Save(ctx, &ghost, 2); !errors.Is(err, studio.ErrProjectNotFound) {
		t.Fatalf("a missing project is not a conflict: %v", err)
	}
}

func TestProjectsAreListedNewestFirstAndArchivedOnesHidden(t *testing.T) {
	db := repotest.IsolatedDB(t, "studio_projects_list", &schema.StudioProject{})
	repo := New(db)
	ctx := context.Background()
	ws := uuid.NewString()
	var ids []string
	for _, name := range []string{"Um", "Dois", "Três"} {
		p, _ := studio.NewProject(ws, uuid.NewString(), studio.KindImage, name, imageDocument(t, "#ffffff"))
		if err := repo.Create(ctx, p); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, p.ID)
	}
	if err := repo.Archive(ctx, ws, ids[1]); err != nil {
		t.Fatal(err)
	}
	list, total, err := repo.List(ctx, studio.ListQuery{WorkspaceID: ws, Kind: studio.KindImage, Limit: 10})
	if err != nil || total != 2 || len(list) != 2 || list[0].Name != "Três" {
		t.Fatalf("list %+v total %d err %v", list, total, err)
	}
	if _, err := repo.Get(ctx, ws, ids[1]); !errors.Is(err, studio.ErrProjectNotFound) {
		t.Fatalf("an archived project is gone: %v", err)
	}
	videos, total, _ := repo.List(ctx, studio.ListQuery{WorkspaceID: ws, Kind: studio.KindVideo, Limit: 10})
	if len(videos) != 0 || total != 0 {
		t.Fatalf("videos %+v", videos)
	}
}
