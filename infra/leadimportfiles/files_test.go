package leadimportfiles

import (
	"context"
	"errors"
	"strings"
	"testing"

	"vozko/domain/leadimport"
	"vozko/domain/media"
)

type memoryObjects struct {
	objects map[string][]byte
	types   map[string]string
	deleted []string
	failPut bool
}

func (m *memoryObjects) UploadFile(key string, data []byte, contentType string) error {
	if m.failPut {
		return errors.New("bucket down")
	}
	m.objects[key], m.types[key] = data, contentType
	return nil
}

func (m *memoryObjects) GetFileURL(key string) string { return "https://files.test/" + key }

func (m *memoryObjects) KeyFromURL(url string) (string, bool) {
	return strings.CutPrefix(url, "https://files.test/")
}

func (m *memoryObjects) DeleteFile(_ context.Context, key string) error {
	delete(m.objects, key)
	m.deleted = append(m.deleted, key)
	return nil
}

type memoryLibrary struct {
	medias  map[string]media.Media
	deleted []string
}

func (l *memoryLibrary) CreateMedia(m *media.Media) error {
	m.ID = "media-" + string(rune('a'+len(l.medias)))
	l.medias[m.ID] = *m
	return nil
}

func (l *memoryLibrary) GetMediaByID(id string) (*media.Media, error) {
	m, ok := l.medias[id]
	if !ok {
		return nil, media.ErrMediaNotFound
	}
	return &m, nil
}

func (l *memoryLibrary) DeleteMedia(id string) error {
	delete(l.medias, id)
	l.deleted = append(l.deleted, id)
	return nil
}

type libraryReader struct {
	lib     *memoryLibrary
	objects *memoryObjects
}

func (r libraryReader) Read(_ context.Context, workspaceID, mediaID string) (*media.Content, error) {
	m, ok := r.lib.medias[mediaID]
	if !ok || m.WorkspaceID != workspaceID {
		return nil, media.ErrMediaNotFound
	}
	key, _ := r.objects.KeyFromURL(m.URL)
	return &media.Content{Media: &m, Data: r.objects.objects[key]}, nil
}

func newFiles() (*Files, *memoryObjects, *memoryLibrary) {
	objects := &memoryObjects{objects: map[string][]byte{}, types: map[string]string{}}
	lib := &memoryLibrary{medias: map[string]media.Media{}}
	files, err := New(objects, lib, libraryReader{lib: lib, objects: objects})
	if err != nil {
		panic(err)
	}
	return files, objects, lib
}

func TestStoreKeepsTheSheetOutOfTheLibraryAndReadsItBack(t *testing.T) {
	files, objects, lib := newFiles()
	ctx := context.Background()
	id, err := files.Store(ctx, "ws-1", "escola.csv", []byte("telefone\n1\n"))
	if err != nil {
		t.Fatal(err)
	}
	stored := lib.medias[id]
	if stored.Type != media.MediaTypeLeadImport || stored.WorkspaceID != "ws-1" || stored.Type.Listed() {
		t.Fatalf("media = %+v", stored)
	}
	key, _ := objects.KeyFromURL(stored.URL)
	if !strings.HasPrefix(key, "lead-imports/ws-1/") || !strings.HasSuffix(key, ".csv") || objects.types[key] != "text/csv; charset=utf-8" {
		t.Fatalf("key = %q, type = %q", key, objects.types[key])
	}
	data, err := files.Read(ctx, "ws-1", id)
	if err != nil || string(data) != "telefone\n1\n" {
		t.Fatalf("read = %q, %v", data, err)
	}
	if _, err := files.Read(ctx, "ws-2", id); !errors.Is(err, leadimport.ErrFileUnavailable) {
		t.Fatalf("another workspace read the sheet: %v", err)
	}
}

func TestEraseRemovesTheObjectAndTheRecord(t *testing.T) {
	files, objects, lib := newFiles()
	ctx := context.Background()
	id, _ := files.Store(ctx, "ws-1", "escola.csv", []byte("telefone\n1\n"))
	if err := files.Erase(ctx, "ws-2", id); !errors.Is(err, leadimport.ErrFileUnavailable) {
		t.Fatalf("erase from another workspace = %v", err)
	}
	if err := files.Erase(ctx, "ws-1", id); err != nil {
		t.Fatal(err)
	}
	if len(objects.objects) != 0 || len(lib.medias) != 0 {
		t.Fatalf("left behind: %v objects, %v records", objects.objects, lib.medias)
	}
	if err := files.Erase(ctx, "ws-1", id); err != nil {
		t.Fatalf("erasing twice must be a no-op: %v", err)
	}
}

func TestStoreReportsAFailedUpload(t *testing.T) {
	files, objects, lib := newFiles()
	objects.failPut = true
	if _, err := files.Store(context.Background(), "ws-1", "escola.csv", []byte("x")); err == nil || len(lib.medias) != 0 {
		t.Fatalf("err = %v, records = %d", err, len(lib.medias))
	}
}

func TestNewRefusesMissingPieces(t *testing.T) {
	if _, err := New(nil, nil, nil); err == nil {
		t.Fatal("an adapter without storage was built")
	}
}

func TestReadLibraryRefusesMediaOutsideTheLibrary(t *testing.T) {
	files, objects, lib := newFiles()
	ctx := context.Background()
	private, _ := files.Store(ctx, "ws-1", "escola.csv", []byte("telefone\n1\n"))
	objects.objects["library/lista.csv"] = []byte("telefone\n2\n")
	lib.medias["lib-1"] = media.Media{ID: "lib-1", WorkspaceID: "ws-1", URL: objects.GetFileURL("library/lista.csv"), Type: media.MediaTypeDocument}
	lib.medias["proxy-1"] = media.Media{ID: "proxy-1", WorkspaceID: "ws-1", URL: objects.GetFileURL("library/lista.csv"), Type: media.MediaTypeStudioProxy}

	if data, err := files.ReadLibrary(ctx, "ws-1", "lib-1"); err != nil || string(data) != "telefone\n2\n" {
		t.Fatalf("library read = %q, %v", data, err)
	}
	for _, id := range []string{private, "proxy-1", "missing"} {
		if _, err := files.ReadLibrary(ctx, "ws-1", id); !errors.Is(err, leadimport.ErrFileUnavailable) {
			t.Fatalf("ReadLibrary(%s) = %v, want unavailable", id, err)
		}
	}
	if _, err := files.ReadLibrary(ctx, "ws-2", "lib-1"); !errors.Is(err, leadimport.ErrFileUnavailable) {
		t.Fatalf("another workspace read a library file: %v", err)
	}
}

type failingReader struct{ err error }

func (r failingReader) Read(context.Context, string, string) (*media.Content, error) {
	return nil, r.err
}

func TestAStorageErrorIsNotAMissingFile(t *testing.T) {
	objects := &memoryObjects{objects: map[string][]byte{}, types: map[string]string{}}
	lib := &memoryLibrary{medias: map[string]media.Media{}}
	outage := errors.New("s3 timeout")
	files, err := New(objects, lib, failingReader{err: outage})
	if err != nil {
		t.Fatal(err)
	}
	for name, read := range map[string]func(context.Context, string, string) ([]byte, error){"read": files.Read, "library": files.ReadLibrary} {
		_, err := read(context.Background(), "ws-1", "media-a")
		if !errors.Is(err, outage) || errors.Is(err, leadimport.ErrFileUnavailable) {
			t.Fatalf("%s = %v, want the storage error as it came", name, err)
		}
	}
}
