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
	var kept bytes.Buffer
	if _, err := io.Copy(&kept, io.LimitReader(body, keptBytes)); err != nil {
		return err
	}
	if _, err := io.Copy(io.Discard, body); err != nil {
		return err
	}
	b.files[key], b.types[key] = kept.Bytes(), contentType
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

const keptBytes = 1 << 20

type zeros struct{}

func (zeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func mp4Stream(size int64) io.Reader {
	head := []byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p'}
	return io.MultiReader(bytes.NewReader(head), io.LimitReader(zeros{}, size-int64(len(head))))
}

func mp4Body(size int) []byte {
	video := make([]byte, size)
	copy(video, []byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p'})
	return video
}

func headed(head string, size int) []byte {
	body := make([]byte, size)
	copy(body, head)
	return body
}

const exportSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10" viewBox="0 0 10 10"><rect x="0" y="0" width="10" height="10" fill="#ffffff"/></svg>`

type exportFixture struct {
	svc      *ExportService
	projects *memoryProjects
	storage  *bucket
	library  *exportLibrary
	video    *studio.Project
	image    *studio.Project
}

func exportService(t *testing.T) exportFixture {
	t.Helper()
	projects := &memoryProjects{items: map[string]*studio.Project{}}
	video, err := studio.NewProject(exportWorkspace, "u", studio.KindVideo, "Reels de segunda", videoJSON(t, false))
	if err != nil {
		t.Fatal(err)
	}
	image, err := studio.NewProject(exportWorkspace, "u", studio.KindImage, "Post", imageJSON(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []*studio.Project{video, image} {
		if err := projects.Create(context.Background(), p); err != nil {
			t.Fatal(err)
		}
	}
	storage, library := &bucket{files: map[string][]byte{}, types: map[string]string{}}, &exportLibrary{}
	svc, err := NewExportService(projects, storage, library)
	if err != nil {
		t.Fatal(err)
	}
	svc.newID = func() string { return exportID }
	return exportFixture{svc: svc, projects: projects, storage: storage, library: library, video: video, image: image}
}

func TestAnExportIsStoredInItsProjectFolderAndJoinsTheLibrary(t *testing.T) {
	f := exportService(t)
	body := mp4Body(4096)
	saved, err := f.svc.Save(context.Background(), exportWorkspace, f.video.ID, "", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	key := studio.ExportKey(exportWorkspace, f.video.ID, exportID, studio.ExportMP4)
	if !bytes.Equal(f.storage.files[key], body) || f.storage.types[key] != "video/mp4" {
		t.Fatalf("stored %d bytes as %q", len(f.storage.files[key]), f.storage.types[key])
	}
	if saved.ID != exportID || saved.Type != media.MediaTypeProductVideo || saved.Description != "Reels de segunda" || saved.URL != "https://cdn.test/"+key {
		t.Fatalf("media %+v", saved)
	}
}

func TestEachExportFormatIsStoredWithItsTypeAndLibraryKind(t *testing.T) {
	cases := []struct {
		project func(exportFixture) *studio.Project
		body    []byte
		format  studio.ExportFormat
		kind    media.MediaType
	}{
		{func(f exportFixture) *studio.Project { return f.video }, headed("GIF89a", 256), studio.ExportGIF, media.MediaTypeProductImage},
		{func(f exportFixture) *studio.Project { return f.video }, headed("\x89PNG\r\n\x1a\n", 256), studio.ExportPNG, media.MediaTypeProductImage},
		{func(f exportFixture) *studio.Project { return f.video }, headed("\xff\xd8\xff\xe0", 256), studio.ExportJPEG, media.MediaTypeProductImage},
		{func(f exportFixture) *studio.Project { return f.video }, headed("RIFF\x00\x00\x00\x00WEBPVP8X\x0a\x00\x00\x00\x02", 256), studio.ExportWebP, media.MediaTypeProductImage},
		{func(f exportFixture) *studio.Project { return f.image }, headed("%PDF-1.4\n", 256), studio.ExportPDF, media.MediaTypeDocumentPdf},
		{func(f exportFixture) *studio.Project { return f.image }, []byte(exportSVG), studio.ExportSVG, media.MediaTypeProductImage},
	}
	for _, c := range cases {
		f := exportService(t)
		project := c.project(f)
		saved, err := f.svc.Save(context.Background(), exportWorkspace, project.ID, "Post Feed", bytes.NewReader(c.body))
		if err != nil {
			t.Fatalf("%s: %v", c.format, err)
		}
		key := studio.ExportKey(exportWorkspace, project.ID, exportID, c.format)
		if !bytes.Equal(f.storage.files[key], c.body) || f.storage.types[key] != c.format.ContentType() {
			t.Fatalf("%s: stored %d bytes as %q", c.format, len(f.storage.files[key]), f.storage.types[key])
		}
		if saved.Type != c.kind || saved.Description != "Post Feed" {
			t.Fatalf("%s: media %+v", c.format, saved)
		}
	}
}

func TestAnExportThatIsNotAKnownFileOrTooLargeIsNeverStored(t *testing.T) {
	f := exportService(t)
	if _, err := f.svc.Save(context.Background(), exportWorkspace, f.video.ID, "", bytes.NewReader([]byte("<html>not a video</html>"))); !errors.Is(err, studio.ErrInvalidExport) {
		t.Fatalf("html: %v", err)
	}
	if _, err := f.svc.Save(context.Background(), exportWorkspace, f.video.ID, "", mp4Stream(studio.MaxExportBytes+1)); !errors.Is(err, studio.ErrExportTooLarge) {
		t.Fatalf("too large: %v", err)
	}
	unsafe := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1"><script>alert(1)</script></svg>`)
	if _, err := f.svc.Save(context.Background(), exportWorkspace, f.image.ID, "", bytes.NewReader(unsafe)); !errors.Is(err, studio.ErrInvalidExport) {
		t.Fatalf("svg with a script: %v", err)
	}
	if len(f.storage.files) != 0 || len(f.library.registered) != 0 {
		t.Fatalf("files %d registered %d", len(f.storage.files), len(f.library.registered))
	}
}

func TestAProjectExportsOnlyTheFormatsOfItsKindAndOnlyInItsWorkspace(t *testing.T) {
	f := exportService(t)
	if _, err := f.svc.Save(context.Background(), exportWorkspace, f.image.ID, "", bytes.NewReader(mp4Body(64))); !errors.Is(err, studio.ErrExportFormat) {
		t.Fatalf("mp4 from an image project: %v", err)
	}
	if _, err := f.svc.Save(context.Background(), exportWorkspace, f.video.ID, "", bytes.NewReader(headed("%PDF-1.4", 64))); !errors.Is(err, studio.ErrExportFormat) {
		t.Fatalf("pdf from a video project: %v", err)
	}
	if _, err := f.svc.Save(context.Background(), "9a8b7c6d-5e4f-4a3b-8c2d-1e0f9a8b7c6d", f.video.ID, "", bytes.NewReader(mp4Body(64))); !errors.Is(err, studio.ErrProjectNotFound) {
		t.Fatalf("other workspace: %v", err)
	}
	if len(f.storage.files) != 0 {
		t.Fatalf("files %d", len(f.storage.files))
	}
}

func TestAnExportWithADescriptionOverTheLimitIsRefused(t *testing.T) {
	f := exportService(t)
	long := string(bytes.Repeat([]byte("a"), studio.MaxExportDescriptionRunes+1))
	if _, err := f.svc.Save(context.Background(), exportWorkspace, f.video.ID, long, bytes.NewReader(mp4Body(64))); !errors.Is(err, studio.ErrInvalidExport) {
		t.Fatalf("long description: %v", err)
	}
}

func TestAnExportTheLibraryRefusesIsRemovedFromStorage(t *testing.T) {
	f := exportService(t)
	f.library.err = errors.New("upload limit reached")
	if _, err := f.svc.Save(context.Background(), exportWorkspace, f.video.ID, "", bytes.NewReader(mp4Body(64))); err == nil {
		t.Fatal("a refused registration must fail the export")
	}
	if len(f.storage.files) != 0 {
		t.Fatalf("files left behind: %d", len(f.storage.files))
	}
}
