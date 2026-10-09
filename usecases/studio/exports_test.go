package studio_usecase

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"vozko/domain/media"
	"vozko/domain/studio"
)

const (
	exportWorkspace = "0b6b3c1e-8d4f-4f6a-9a2e-1c3d5e7f9a0b"
	exportID        = "6f1c2a8e-3b4d-4e5f-8a9b-0c1d2e3f4a5b"
)

type bucket struct {
	files map[string][]byte
	types map[string]string
}

func (b *bucket) PutStream(_ context.Context, key, contentType string, body io.Reader) error {
	data, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	b.files[key], b.types[key] = data, contentType
	return nil
}

func (b *bucket) DeleteFile(_ context.Context, key string) error {
	delete(b.files, key)
	return nil
}

func (b *bucket) GetFileURL(key string) string { return "https://cdn.test/" + key }

type exportLibrary struct {
	registered []media.Media
	err        error
}

func (l *exportLibrary) RegisterStored(workspaceID, id, key string, mediaType media.MediaType, description string) (media.Media, error) {
	if l.err != nil {
		return media.Media{}, l.err
	}
	m := media.Media{ID: id, WorkspaceID: workspaceID, URL: "https://cdn.test/" + key, Type: mediaType, Description: description}
	l.registered = append(l.registered, m)
	return m, nil
}

func mp4Body(size int) []byte {
	video := make([]byte, size)
	copy(video, []byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p'})
	return video
}

func exportService(t *testing.T) (*ExportService, *memoryProjects, *bucket, *exportLibrary, *studio.Project) {
	t.Helper()
	projects := &memoryProjects{items: map[string]*studio.Project{}}
	video, err := studio.NewProject(exportWorkspace, "u", studio.KindVideo, "Reels de segunda", videoJSON(t, false))
	if err != nil {
		t.Fatal(err)
	}
	if err := projects.Create(context.Background(), video); err != nil {
		t.Fatal(err)
	}
	storage, library := &bucket{files: map[string][]byte{}, types: map[string]string{}}, &exportLibrary{}
	svc, err := NewExportService(projects, storage, library)
	if err != nil {
		t.Fatal(err)
	}
	svc.newID = func() string { return exportID }
	return svc, projects, storage, library, video
}

func TestAnExportIsStoredInItsProjectFolderAndJoinsTheLibrary(t *testing.T) {
	svc, _, storage, _, video := exportService(t)
	body := mp4Body(4096)
	saved, err := svc.Save(context.Background(), exportWorkspace, video.ID, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	key := studio.ExportKey(exportWorkspace, video.ID, exportID)
	if !bytes.Equal(storage.files[key], body) || storage.types[key] != "video/mp4" {
		t.Fatalf("stored %d bytes as %q", len(storage.files[key]), storage.types[key])
	}
	if saved.ID != exportID || saved.Type != media.MediaTypeProductVideo || saved.Description != "Reels de segunda" || saved.URL != "https://cdn.test/"+key {
		t.Fatalf("media %+v", saved)
	}
}

func TestAnExportThatIsNotAnMP4OrTooLargeIsNeverStored(t *testing.T) {
	svc, _, storage, library, video := exportService(t)
	if _, err := svc.Save(context.Background(), exportWorkspace, video.ID, bytes.NewReader([]byte("<html>not a video</html>"))); !errors.Is(err, studio.ErrInvalidExport) {
		t.Fatalf("html: %v", err)
	}
	if _, err := svc.Save(context.Background(), exportWorkspace, video.ID, bytes.NewReader(mp4Body(studio.MaxExportBytes+1))); !errors.Is(err, studio.ErrExportTooLarge) {
		t.Fatalf("too large: %v", err)
	}
	if len(storage.files) != 0 || len(library.registered) != 0 {
		t.Fatalf("files %d registered %d", len(storage.files), len(library.registered))
	}
}

func TestOnlyVideoProjectsOfTheWorkspaceCanExport(t *testing.T) {
	svc, projects, storage, _, video := exportService(t)
	image, _ := studio.NewProject(exportWorkspace, "u", studio.KindImage, "Post", imageJSON(t))
	_ = projects.Create(context.Background(), image)
	if _, err := svc.Save(context.Background(), exportWorkspace, image.ID, bytes.NewReader(mp4Body(64))); !errors.Is(err, studio.ErrNotVideo) {
		t.Fatalf("image project: %v", err)
	}
	if _, err := svc.Save(context.Background(), "9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d", video.ID, bytes.NewReader(mp4Body(64))); !errors.Is(err, studio.ErrProjectNotFound) {
		t.Fatalf("other workspace: %v", err)
	}
	if len(storage.files) != 0 {
		t.Fatalf("files %d", len(storage.files))
	}
}

func TestAnExportTheLibraryRefusesIsRemovedFromStorage(t *testing.T) {
	svc, _, storage, library, video := exportService(t)
	library.err = errors.New("upload limit reached")
	if _, err := svc.Save(context.Background(), exportWorkspace, video.ID, bytes.NewReader(mp4Body(64))); err == nil {
		t.Fatal("a refused registration must fail the export")
	}
	if len(storage.files) != 0 {
		t.Fatalf("files left behind: %d", len(storage.files))
	}
}
