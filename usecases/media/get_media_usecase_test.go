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
