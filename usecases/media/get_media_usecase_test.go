package media_usecase

import (
	"errors"
	"testing"

	"vozko/domain/media"
)

type mediaByID struct {
	media.MediaRepository
	stored *media.Media
}

func (r mediaByID) GetMediaByID(string) (*media.Media, error) { return r.stored, nil }

func TestGetMediaHidesAnotherWorkspacesMedia(t *testing.T) {
	uc := NewGetMediaUseCase(mediaByID{stored: &media.Media{ID: "m1", WorkspaceID: "ws2"}})
	if _, err := uc.GetMedia("ws1", "m1"); !errors.Is(err, media.ErrMediaNotFound) {
		t.Fatalf("err = %v", err)
	}
	if _, err := uc.GetMedia("", "m1"); !errors.Is(err, media.ErrMediaNotFound) {
		t.Fatalf("no workspace: err = %v", err)
	}
	if got, err := uc.GetMedia("ws2", "m1"); err != nil || got.ID != "m1" {
		t.Fatalf("own media: %v %v", got, err)
	}
}

func TestGetMediaHidesALeadImportSheet(t *testing.T) {
	uc := NewGetMediaUseCase(mediaByID{stored: &media.Media{ID: "m1", WorkspaceID: "ws1", Type: media.MediaTypeLeadImport}})
	if _, err := uc.GetMedia("ws1", "m1"); !errors.Is(err, media.ErrMediaNotFound) {
		t.Fatalf("err = %v, want not found", err)
	}
}

func TestGetMediaKeepsAStudioProxyOfTheSameWorkspaceReadable(t *testing.T) {
	proxy := mediaByID{stored: &media.Media{ID: "p1", WorkspaceID: "ws1", Type: media.MediaTypeStudioProxy}}
	if got, err := NewGetMediaUseCase(proxy).GetMedia("ws1", "p1"); err != nil || got.ID != "p1" {
		t.Fatalf("studio proxy: %v %v", got, err)
	}
	if _, err := NewGetMediaUseCase(proxy).GetMedia("ws2", "p1"); !errors.Is(err, media.ErrMediaNotFound) {
		t.Fatalf("another workspace's proxy: err = %v", err)
	}
}

func TestGetMediaReadsALeadImportSheetOnlyWhenTheCallerNamesIt(t *testing.T) {
	sheet := mediaByID{stored: &media.Media{ID: "m1", WorkspaceID: "ws1", Type: media.MediaTypeLeadImport}}
	if got, err := NewGetMediaUseCase(sheet, media.MediaTypeLeadImport).GetMedia("ws1", "m1"); err != nil || got.ID != "m1" {
		t.Fatalf("named kind: %v %v", got, err)
	}
	listed := mediaByID{stored: &media.Media{ID: "m3", WorkspaceID: "ws1", Type: media.MediaTypeDocument}}
	if got, err := NewGetMediaUseCase(listed, media.MediaTypeLeadImport).GetMedia("ws1", "m3"); err != nil || got.ID != "m3" {
		t.Fatalf("library file: %v %v", got, err)
	}
}
