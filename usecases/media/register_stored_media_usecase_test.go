package media_usecase

import (
	"errors"
	"testing"

	"vozko/domain/media"
)

type libraryRows struct {
	media.MediaRepository
	rows  map[string]*media.Media
	count int64
}

func (r *libraryRows) CountByWorkspaceID(string) (int64, error) { return r.count, nil }

func (r *libraryRows) CreateMedia(m *media.Media) error {
	r.rows[m.ID] = m
	return nil
}

type cdnFiles struct{ media.FileStorage }

func (cdnFiles) GetFileURL(key string) string { return "https://cdn.test/" + key }

func TestAStoredFileIsRegisteredUnderItsGivenID(t *testing.T) {
	rows := &libraryRows{rows: map[string]*media.Media{}}
	uc := NewRegisterStoredMediaUseCase(rows, cdnFiles{})
	first, err := uc.RegisterStored("ws1", "6f1c2a8e-3b4d-4e5f-8a9b-0c1d2e3f4a5b", "studio-exports/ws1/p1/x.mp4", media.MediaTypeProductVideo, "Reels")
	if err != nil || first.URL != "https://cdn.test/studio-exports/ws1/p1/x.mp4" || first.Type != media.MediaTypeProductVideo || first.Description != "Reels" {
		t.Fatalf("first = %+v err %v", first, err)
	}
	if rows.rows["6f1c2a8e-3b4d-4e5f-8a9b-0c1d2e3f4a5b"] == nil {
		t.Fatalf("rows %v", rows.rows)
	}
}

func TestRegisteringFollowsTheLibraryLimits(t *testing.T) {
	full := &libraryRows{rows: map[string]*media.Media{}, count: maxWorkspaceUploads}
	if _, err := NewRegisterStoredMediaUseCase(full, cdnFiles{}).RegisterStored("ws1", "6f1c2a8e-3b4d-4e5f-8a9b-0c1d2e3f4a5b", "k", media.MediaTypeProductVideo, "x"); !errors.Is(err, ErrUploadLimit) {
		t.Fatalf("full library: %v", err)
	}
	if _, err := NewRegisterStoredMediaUseCase(&libraryRows{rows: map[string]*media.Media{}}, cdnFiles{}).RegisterStored("ws1", "6f1c2a8e-3b4d-4e5f-8a9b-0c1d2e3f4a5b", "k", media.MediaTypeStudioProxy, "x"); err == nil {
		t.Fatal("a hidden type cannot be registered as a library upload")
	}
}
