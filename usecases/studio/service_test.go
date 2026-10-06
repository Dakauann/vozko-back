package studio_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"vozko/domain/mediagen"
	"vozko/domain/studio"
)

type memoryProjects struct {
	items map[string]*studio.Project
	seq   int
}

func (m *memoryProjects) Create(_ context.Context, p *studio.Project) error {
	m.seq++
	p.ID = string(rune('a' + m.seq))
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

type recordingMedia struct{ requested []mediagen.Request }

func (r *recordingMedia) Request(_ context.Context, req mediagen.Request, _ string) (*mediagen.Job, error) {
	r.requested = append(r.requested, req)
	return &mediagen.Job{ID: "job-1", Kind: req.Kind, Status: mediagen.StatusQueued}, nil
}

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

func service(t *testing.T) (*Service, *memoryProjects, *recordingMedia) {
	t.Helper()
	projects, media := &memoryProjects{items: map[string]*studio.Project{}}, &recordingMedia{}
	svc, err := NewService(projects, media)
	if err != nil {
		t.Fatal(err)
	}
	return svc, projects, media
}

func TestSavingFromAStaleVersionIsAConflict(t *testing.T) {
	svc, _, _ := service(t)
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

func TestExportRendersOnlyTheSavedVersionWithItsRasters(t *testing.T) {
	svc, _, media := service(t)
	ctx := context.Background()
	p, _ := svc.Create(ctx, "ws", "u", studio.KindVideo, "Reels", videoJSON(t, true))
	if _, err := svc.Export(ctx, "ws", "u", p.ID, 2, map[string]string{"o1": "r-1"}); !errors.Is(err, studio.ErrVersionConflict) {
		t.Fatalf("exported an unsaved version: %v", err)
	}
	if _, err := svc.Export(ctx, "ws", "u", p.ID, 1, nil); !errors.Is(err, studio.ErrNotRasterized) {
		t.Fatalf("exported without rasters: %v", err)
	}
	job, err := svc.Export(ctx, "ws", "u", p.ID, 1, map[string]string{"o1": "r-1"})
	if err != nil || job.Kind != mediagen.KindVideo || len(media.requested) != 1 || media.requested[0].Video.Visual[1].Clips[0].MediaID != "r-1" {
		t.Fatalf("job %+v requested %+v err %v", job, media.requested, err)
	}
}
