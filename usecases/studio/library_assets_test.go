package studio_usecase

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"

	"vozko/domain/media"
	"vozko/domain/studio/library"
	media_usecase "vozko/usecases/media"
)

var rocket = []byte("RIFF....WEBPVP8 rocket pixels")

func rocketCatalog(t *testing.T) *library.Catalog {
	t.Helper()
	sum := sha256.Sum256(rocket)
	raw := fmt.Sprintf(`{"version":2,"categories":[{"id":"threeD","panel":"elements","names":{"pt":"3D","en":"3D","es":"3D","de":"3D"}}],"items":[
{"id":"3d-icon-rocket","category":"threeD","panel":"elements","editors":["image","video"],"kind":"asset","ratio":1,"names":{"pt":"Foguete","en":"Rocket","es":"Cohete","de":"Rakete"},"colors":{},"source":{"set":"3dicons","license":"CC0-1.0"},"asset":{"path":"images/studio/library-3d/3d-icon-rocket.webp","sha256":"%s","width":256,"height":256,"contentType":"image/webp"}},
{"id":"shape-heart","category":"threeD","panel":"elements","editors":["image"],"kind":"layers","ratio":1,"names":{"pt":"Coração","en":"Heart","es":"Corazón","de":"Herz"},"colors":{"image":["#111111"]},"source":{"set":"vozko","license":"own"}}]}`, hex.EncodeToString(sum[:]))
	c, err := library.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

type assetLibrary struct {
	rows      map[string]media.Media
	gone      map[string]bool
	failWith  error
	racing    bool
	registers int
}

func (l *assetLibrary) RegisterStored(workspaceID, id, key string, mediaType media.MediaType, description string) (media.Media, error) {
	l.registers++
	if l.failWith != nil {
		return media.Media{}, l.failWith
	}
	if l.racing {
		l.rows[id] = media.Media{ID: id, WorkspaceID: workspaceID, URL: "https://cdn.test/" + key, Type: mediaType, Description: description}
		return media.Media{}, errors.New("duplicate key")
	}
	if _, taken := l.rows[id]; taken || l.gone[id] {
		return media.Media{}, errors.New("duplicate key")
	}
	m := media.Media{ID: id, WorkspaceID: workspaceID, URL: "https://cdn.test/" + key, Type: mediaType, Description: description}
	l.rows[id] = m
	return m, nil
}

func (l *assetLibrary) GetMedia(workspaceID, mediaID string) (*media.Media, error) {
	m, ok := l.rows[mediaID]
	if !ok || m.WorkspaceID != workspaceID {
		return nil, media.ErrMediaNotFound
	}
	return &m, nil
}

func assetService(t *testing.T, lib *assetLibrary) (*LibraryAssetService, *bucket) {
	t.Helper()
	store := &bucket{files: map[string][]byte{}, types: map[string]string{}}
	service, err := NewLibraryAssetService(rocketCatalog(t), store, lib)
	if err != nil {
		t.Fatal(err)
	}
	return service, store
}

func TestALibraryPictureLandsInTheWorkspaceLibraryOnce(t *testing.T) {
	lib := &assetLibrary{rows: map[string]media.Media{}, gone: map[string]bool{}}
	service, store := assetService(t, lib)
	first, err := service.Import(context.Background(), exportWorkspace, "3d-icon-rocket", "pt-BR", bytes.NewReader(rocket))
	if err != nil {
		t.Fatal(err)
	}
	if first.Type != media.MediaTypeProductImage || first.Description != "Foguete" || len(store.files) != 1 {
		t.Fatalf("first = %+v, files = %v", first, store.files)
	}
	for key, kind := range store.types {
		if kind != "image/webp" || !bytes.Equal(store.files[key], rocket) {
			t.Fatalf("stored %s as %s", key, kind)
		}
	}
	again, err := service.Import(context.Background(), exportWorkspace, "3d-icon-rocket", "en", bytes.NewReader(rocket))
	if err != nil || again.ID != first.ID || lib.registers != 1 {
		t.Fatalf("again = %+v, %v after %d registers", again, err, lib.registers)
	}
	other, err := service.Import(context.Background(), exportID, "3d-icon-rocket", "en", bytes.NewReader(rocket))
	if err != nil || other.ID == first.ID || other.Description != "Rocket" {
		t.Fatalf("other workspace = %+v, %v", other, err)
	}
}

func TestADeletedLibraryPictureComesBackUnderANewRow(t *testing.T) {
	lib := &assetLibrary{rows: map[string]media.Media{}, gone: map[string]bool{}}
	service, _ := assetService(t, lib)
	first, err := service.Import(context.Background(), exportWorkspace, "3d-icon-rocket", "pt", bytes.NewReader(rocket))
	if err != nil {
		t.Fatal(err)
	}
	delete(lib.rows, first.ID)
	lib.gone[first.ID] = true
	back, err := service.Import(context.Background(), exportWorkspace, "3d-icon-rocket", "pt", bytes.NewReader(rocket))
	if err != nil || back.ID == first.ID {
		t.Fatalf("back = %+v, %v", back, err)
	}
	same, err := service.Import(context.Background(), exportWorkspace, "3d-icon-rocket", "pt", bytes.NewReader(rocket))
	if err != nil || same.ID != back.ID {
		t.Fatalf("same = %+v, %v", same, err)
	}
}

func TestOnlyTheExactPictureOfAPictureItemIsImported(t *testing.T) {
	lib := &assetLibrary{rows: map[string]media.Media{}, gone: map[string]bool{}}
	service, store := assetService(t, lib)
	cases := map[string]struct {
		item string
		body []byte
		want error
	}{
		"unknown item":   {"3d-icon-unicorn", rocket, library.ErrUnknownAsset},
		"vector item":    {"shape-heart", rocket, library.ErrUnknownAsset},
		"other bytes":    {"3d-icon-rocket", []byte("RIFF....WEBPVP8 something else"), library.ErrAssetMismatch},
		"too many bytes": {"3d-icon-rocket", append(append([]byte{}, rocket...), make([]byte, library.MaxAssetBytes)...), library.ErrAssetMismatch},
	}
	for name, tc := range cases {
		if _, err := service.Import(context.Background(), exportWorkspace, tc.item, "pt", bytes.NewReader(tc.body)); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if len(store.files) != 0 || lib.registers != 0 {
		t.Fatalf("refused imports stored %d files and %d rows", len(store.files), lib.registers)
	}
}

func TestALibraryImportStopsAtTheUploadLimitAndCleansUp(t *testing.T) {
	lib := &assetLibrary{rows: map[string]media.Media{}, gone: map[string]bool{}, failWith: media_usecase.ErrUploadLimit}
	service, store := assetService(t, lib)
	if _, err := service.Import(context.Background(), exportWorkspace, "3d-icon-rocket", "pt", bytes.NewReader(rocket)); !errors.Is(err, media_usecase.ErrUploadLimit) {
		t.Fatalf("err = %v", err)
	}
	if len(store.files) != 0 || lib.registers != 1 {
		t.Fatalf("files = %d, registers = %d", len(store.files), lib.registers)
	}
}

func TestTwoImportsAtOnceKeepTheFileTheWinnerPointsAt(t *testing.T) {
	lib := &assetLibrary{rows: map[string]media.Media{}, gone: map[string]bool{}, racing: true}
	service, store := assetService(t, lib)
	saved, err := service.Import(context.Background(), exportWorkspace, "3d-icon-rocket", "pt", bytes.NewReader(rocket))
	if err != nil || saved.ID == "" {
		t.Fatalf("saved = %+v, %v", saved, err)
	}
	if len(store.files) != 1 {
		t.Fatalf("the losing import deleted the file the saved picture uses: %v", store.files)
	}
}

func TestTheLibraryAssetServiceNeedsItsParts(t *testing.T) {
	if _, err := NewLibraryAssetService(nil, nil, nil); !errors.Is(err, ErrMissingDependency) {
		t.Fatalf("err = %v", err)
	}
}
