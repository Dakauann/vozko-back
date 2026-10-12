package studio_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"vozko/domain/studio"
)

type memoryPresets struct {
	items   []*studio.AnimationPreset
	saved   int
	deleted []string
}

func (m *memoryPresets) CreatePreset(_ context.Context, p *studio.AnimationPreset) error {
	p.ID = uuid.NewString()
	copied := *p
	m.items = append([]*studio.AnimationPreset{&copied}, m.items...)
	return nil
}

func (m *memoryPresets) ListPresets(_ context.Context, ws string) ([]studio.AnimationPreset, error) {
	var out []studio.AnimationPreset
	for _, p := range m.items {
		if p.WorkspaceID == ws {
			out = append(out, *p)
		}
	}
	return out, nil
}

func (m *memoryPresets) GetPreset(_ context.Context, ws, id string) (*studio.AnimationPreset, error) {
	for _, p := range m.items {
		if p.ID == id && p.WorkspaceID == ws {
			copied := *p
			return &copied, nil
		}
	}
	return nil, studio.ErrPresetNotFound
}

func (m *memoryPresets) SavePreset(_ context.Context, p *studio.AnimationPreset) error {
	m.saved++
	for i, stored := range m.items {
		if stored.ID == p.ID {
			copied := *p
			m.items[i] = &copied
		}
	}
	return nil
}

func (m *memoryPresets) DeletePreset(_ context.Context, ws, id string) error {
	if _, err := m.GetPreset(context.Background(), ws, id); err != nil {
		return err
	}
	m.deleted = append(m.deleted, id)
	return nil
}

func presetService(t *testing.T) (*PresetService, *memoryPresets) {
	t.Helper()
	repo := &memoryPresets{}
	svc, err := NewPresetService(repo)
	if err != nil {
		t.Fatal(err)
	}
	return svc, repo
}

var spinAnimation = json.RawMessage(`{"scope":"visual","durationMs":1000,"keyframes":{"rotation":[{"atMs":0,"value":0,"easing":"easeOut"},{"atMs":1000,"value":360,"easing":"linear"}]}}`)

func TestThePresetServiceNeedsItsRepository(t *testing.T) {
	if _, err := NewPresetService(nil); !errors.Is(err, ErrMissingDependency) {
		t.Fatalf("err = %v", err)
	}
}

func TestBuiltinsComeBeforeTheWorkspacePresets(t *testing.T) {
	svc, _ := presetService(t)
	created, err := svc.Create(context.Background(), "ws-1", "u-1", " Giro ", "", spinAnimation)
	if err != nil || created.Name != "Giro" {
		t.Fatalf("created %+v err %v", created, err)
	}
	if _, err := svc.Create(context.Background(), "ws-2", "u-1", "Alheio", "", spinAnimation); err != nil {
		t.Fatal(err)
	}
	list, err := svc.List(context.Background(), "ws-1")
	if err != nil {
		t.Fatal(err)
	}
	builtins, _ := studio.BuiltinPresets()
	if len(list) != len(builtins)+1 || !list[0].BuiltIn || list[len(list)-1].ID != created.ID {
		t.Fatalf("list = %+v", list)
	}
	for _, p := range list[:len(builtins)] {
		if !p.BuiltIn {
			t.Fatalf("a saved preset came before a built-in: %+v", p)
		}
	}
}

func TestAnInvalidPresetNeverReachesTheRepository(t *testing.T) {
	svc, repo := presetService(t)
	_, err := svc.Create(context.Background(), "ws-1", "u-1", "Nada", "", json.RawMessage(`{"scope":"visual","durationMs":1000}`))
	var invalid *studio.ValidationError
	if !errors.As(err, &invalid) || len(repo.items) != 0 {
		t.Fatalf("err %v items %d", err, len(repo.items))
	}
}

func TestBuiltinPresetsResolveButStayReadOnly(t *testing.T) {
	svc, repo := presetService(t)
	ctx := context.Background()
	got, err := svc.Get(ctx, "ws-1", "builtin.riseIn")
	if err != nil || !got.BuiltIn || got.Names["en"] != "Rise in" {
		t.Fatalf("got %+v err %v", got, err)
	}
	if _, err := svc.Get(ctx, "ws-1", "builtin.missing"); !errors.Is(err, studio.ErrPresetNotFound) {
		t.Fatalf("missing built-in %v", err)
	}
	name := "Meu"
	if _, err := svc.Update(ctx, "ws-1", "builtin.riseIn", studio.PresetChange{Name: &name}); !errors.Is(err, studio.ErrPresetReadOnly) {
		t.Fatalf("update %v", err)
	}
	if err := svc.Delete(ctx, "ws-1", "builtin.riseIn"); !errors.Is(err, studio.ErrPresetReadOnly) {
		t.Fatalf("delete %v", err)
	}
	if repo.saved != 0 || len(repo.deleted) != 0 {
		t.Fatal("a built-in reached the repository")
	}
}

func TestAWorkspacePresetIsRenamedAndDeleted(t *testing.T) {
	svc, repo := presetService(t)
	ctx := context.Background()
	created, _ := svc.Create(ctx, "ws-1", "u-1", "Giro", "", spinAnimation)
	name, description := "Giro rápido", "Uma volta"
	updated, err := svc.Update(ctx, "ws-1", created.ID, studio.PresetChange{Name: &name, Description: &description})
	if err != nil || updated.Name != name || updated.Description != description || repo.saved != 1 {
		t.Fatalf("updated %+v err %v", updated, err)
	}
	if _, err := svc.Update(ctx, "ws-1", created.ID, studio.PresetChange{}); err == nil || repo.saved != 1 {
		t.Fatalf("an empty change was saved: %v", err)
	}
	if _, err := svc.Update(ctx, "ws-2", created.ID, studio.PresetChange{Name: &name}); !errors.Is(err, studio.ErrPresetNotFound) {
		t.Fatalf("another workspace %v", err)
	}
	fetched, err := svc.Get(ctx, "ws-1", created.ID)
	if err != nil || fetched.Name != name {
		t.Fatalf("fetched %+v err %v", fetched, err)
	}
	if err := svc.Delete(ctx, "ws-1", created.ID); err != nil || len(repo.deleted) != 1 {
		t.Fatalf("delete %v", err)
	}
}
