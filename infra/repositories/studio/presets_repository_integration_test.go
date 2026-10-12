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

func presetAnimation() json.RawMessage {
	return json.RawMessage(`{"scope":"visual","durationMs":1000,"motionBlur":true}`)
}

func TestPresetsAreListedNewestFirstAndStayInTheirWorkspace(t *testing.T) {
	db := repotest.IsolatedDB(t, "studio_presets_list", &schema.StudioAnimationPreset{})
	repo := NewPresets(db)
	ctx := context.Background()
	ws := uuid.NewString()
	var ids []string
	for _, name := range []string{"Um", "Dois", "Três"} {
		p, err := studio.NewAnimationPreset(ws, uuid.NewString(), name, "", presetAnimation())
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.CreatePreset(ctx, p); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, p.ID)
	}
	other, _ := studio.NewAnimationPreset(uuid.NewString(), uuid.NewString(), "Outro", "", presetAnimation())
	if err := repo.CreatePreset(ctx, other); err != nil {
		t.Fatal(err)
	}
	list, err := repo.ListPresets(ctx, ws)
	if err != nil || len(list) != 3 || list[0].Name != "Três" || list[2].Name != "Um" {
		t.Fatalf("list %+v err %v", list, err)
	}
	var animation map[string]any
	if err := json.Unmarshal(list[0].Animation, &animation); err != nil || animation["scope"] != "visual" {
		t.Fatalf("animation %s", list[0].Animation)
	}
	if _, err := repo.GetPreset(ctx, ws, other.ID); !errors.Is(err, studio.ErrPresetNotFound) {
		t.Fatalf("another workspace read the preset: %v", err)
	}
	if _, err := repo.GetPreset(ctx, ws, "builtin.popIn"); !errors.Is(err, studio.ErrPresetNotFound) {
		t.Fatalf("a malformed id: %v", err)
	}
	got, err := repo.GetPreset(ctx, ws, ids[1])
	if err != nil || got.Name != "Dois" {
		t.Fatalf("get %+v err %v", got, err)
	}
}

func TestAPresetIsRenamedAndDeletedOnlyInsideItsWorkspace(t *testing.T) {
	db := repotest.IsolatedDB(t, "studio_presets_change", &schema.StudioAnimationPreset{})
	repo := NewPresets(db)
	ctx := context.Background()
	ws := uuid.NewString()
	p, _ := studio.NewAnimationPreset(ws, uuid.NewString(), "Antes", "", presetAnimation())
	if err := repo.CreatePreset(ctx, p); err != nil {
		t.Fatal(err)
	}
	stranger := *p
	stranger.WorkspaceID = uuid.NewString()
	stranger.Name = "Invasor"
	if err := repo.SavePreset(ctx, &stranger); !errors.Is(err, studio.ErrPresetNotFound) {
		t.Fatalf("another workspace renamed the preset: %v", err)
	}
	p.Name, p.Description = "Depois", "Nova descrição"
	if err := repo.SavePreset(ctx, p); err != nil {
		t.Fatal(err)
	}
	stored, _ := repo.GetPreset(ctx, ws, p.ID)
	if stored.Name != "Depois" || stored.Description != "Nova descrição" {
		t.Fatalf("stored %+v", stored)
	}
	if err := repo.DeletePreset(ctx, uuid.NewString(), p.ID); !errors.Is(err, studio.ErrPresetNotFound) {
		t.Fatalf("another workspace deleted the preset: %v", err)
	}
	if err := repo.DeletePreset(ctx, ws, p.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeletePreset(ctx, ws, p.ID); !errors.Is(err, studio.ErrPresetNotFound) {
		t.Fatalf("a second delete: %v", err)
	}
	var count int64
	db.Model(&schema.StudioAnimationPreset{}).Where("id = ?", p.ID).Count(&count)
	if count != 0 {
		t.Fatal("the delete left the row behind")
	}
}
