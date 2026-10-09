package studio_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"vozko/domain/mediagen"
	"vozko/domain/shared"
	"vozko/domain/studio"
)

type memoryProjects struct {
	items map[string]*studio.Project
}

func (m *memoryProjects) Create(_ context.Context, p *studio.Project) error {
	p.ID = uuid.NewString()
	copied := *p
	m.items[p.ID] = &copied
	return nil
}

func (m *memoryProjects) Get(_ context.Context, ws, id string) (*studio.Project, error) {
	p, ok := m.items[id]
	if !ok || p.WorkspaceID != ws {
		return nil, studio.ErrProjectNotFound
	}
	copied := *p
	return &copied, nil
}

func (m *memoryProjects) List(context.Context, studio.ListQuery) ([]studio.Summary, int64, error) {
	return nil, 0, nil
}

func (m *memoryProjects) Save(_ context.Context, p *studio.Project, expected int64) error {
	stored := m.items[p.ID]
	if stored.Version != expected {
		return studio.ErrVersionConflict
	}
	p.Version = expected + 1
	copied := *p
	m.items[p.ID] = &copied
	return nil
}

func (m *memoryProjects) Archive(context.Context, string, string) error { return nil }

func videoJSON(t *testing.T, withOverlay bool) json.RawMessage {
	t.Helper()
	tracks := []studio.Track{{ID: "main", Kind: studio.TrackVisual, Clips: []studio.Clip{
		{ID: "c1", Type: studio.ClipImage, AssetID: "m-1", DurationMS: 3_000, Fit: mediagen.FitCover, Transform: mediagen.FullFrame()},
	}}}
	if withOverlay {
		layer := studio.Layer{ID: "t", Type: studio.LayerText, Text: "Oi", FontID: "inter", FontSize: 0.05, Transform: studio.Transform{X: 0.5, Y: 0.5, W: 0.5, H: 0.1, Opacity: 1}}
		tracks = append(tracks, studio.Track{ID: "ovl", Kind: studio.TrackVisual, Clips: []studio.Clip{
			{ID: "o1", Type: studio.ClipOverlay, DurationMS: 1_000, Layer: &layer, Transform: mediagen.Transform{X: 0.5, Y: 0.5, W: 0.5, H: 0.1, Opacity: 1}},
		}})
	}
	raw, err := json.Marshal(studio.VideoDocument{Schema: studio.SchemaVideo, Version: studio.DocumentVersion, DurationMS: 3_000,
		Canvas: studio.VideoCanvas{Aspect: mediagen.AspectSquare, Background: "#000000"}, Tracks: tracks})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func imageJSON(t *testing.T) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(studio.LegacyImageDocument{Schema: studio.SchemaImage, Version: studio.DocumentVersion,
		Canvas: studio.Canvas{Width: 1080, Height: 1080, Background: "#ffffff"}, Layers: []studio.Layer{}})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func service(t *testing.T) (*Service, *memoryProjects) {
	t.Helper()
	projects := &memoryProjects{items: map[string]*studio.Project{}}
	svc, err := NewService(projects)
	if err != nil {
		t.Fatal(err)
	}
	return svc, projects
}

func TestSavingFromAStaleVersionIsAConflict(t *testing.T) {
	svc, _ := service(t)
	ctx := context.Background()
	p, err := svc.Create(ctx, "ws", "u", studio.KindVideo, "Reels", videoJSON(t, false))
	if err != nil {
		t.Fatal(err)
	}
	saved, err := svc.Save(ctx, "ws", p.ID, 1, studio.Change{Document: videoJSON(t, true)})
	if err != nil || saved.Version != 2 {
		t.Fatalf("saved %+v err %v", saved, err)
	}
	if _, err := svc.Save(ctx, "ws", p.ID, 1, studio.Change{Document: videoJSON(t, false)}); !errors.Is(err, studio.ErrVersionConflict) {
		t.Fatalf("got %v", err)
	}
	if _, err := svc.Save(ctx, "other", p.ID, 2, studio.Change{Document: videoJSON(t, false)}); !errors.Is(err, studio.ErrProjectNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestSavingRefusesAMissingVersion(t *testing.T) {
	svc, _ := service(t)
	ctx := context.Background()
	p, err := svc.Create(ctx, "ws", "u", studio.KindVideo, "Reels", videoJSON(t, false))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Save(ctx, "ws", p.ID, 0, studio.Change{Document: videoJSON(t, true)}); !errors.Is(err, shared.ErrVersionRequired) {
		t.Fatalf("save without a version: %v", err)
	}
}

func TestStudioConflictIsTheSharedVersionConflict(t *testing.T) {
	if !errors.Is(studio.ErrVersionConflict, shared.ErrVersionConflict) {
		t.Fatal("studio must reuse the shared optimistic lock sentinel")
	}
}
