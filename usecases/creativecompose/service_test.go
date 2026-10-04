package creativecompose

import (
	"context"
	"errors"
	"testing"

	domain "vozko/domain/creativecompose"
	"vozko/domain/media"
)

type fakeRenderer struct {
	layout domain.Layout
	images domain.Images
	err    error
}

func (r *fakeRenderer) Render(_ context.Context, layout domain.Layout, images domain.Images) ([]byte, error) {
	r.layout, r.images = layout, images
	return []byte("png"), r.err
}

type workspaceMedia map[string]*media.Media

func (w workspaceMedia) GetMedia(workspaceID, mediaID string) (*media.Media, error) {
	m, ok := w[mediaID]
	if !ok || m.WorkspaceID != workspaceID {
		return nil, media.ErrMediaNotFound
	}
	return m, nil
}

type fakeUploader struct{ uploaded []string }

func (u *fakeUploader) UploadMedia(workspaceID string, data []byte, name string, kind media.MediaType, description string) (media.Media, error) {
	u.uploaded = append(u.uploaded, name)
	return media.Media{ID: "new-media", WorkspaceID: workspaceID, URL: "https://cdn/new.png", Type: kind}, nil
}

var files = workspaceMedia{
	"photo": {ID: "photo", WorkspaceID: "ws1", URL: "https://cdn/photo.jpg", Type: media.MediaTypeProductImage},
	"logo":  {ID: "logo", WorkspaceID: "ws1", URL: "https://cdn/logo.png", Type: media.MediaTypeProductImage},
	"clip":  {ID: "clip", WorkspaceID: "ws1", URL: "https://cdn/clip.mp4", Type: media.MediaTypeProductVideo},
	"other": {ID: "other", WorkspaceID: "ws2", URL: "https://cdn/other.jpg", Type: media.MediaTypeProductImage},
}

func service() (*Service, *fakeRenderer, *fakeUploader) {
	r, u := &fakeRenderer{}, &fakeUploader{}
	return NewService(r, files, u), r, u
}

func layout() domain.Layout {
	return domain.Layout{Template: domain.TemplateCard, Headline: " Todos os canais ", ImageMediaID: "photo", LogoMediaID: "logo"}
}

func TestComposeRendersTheLayoutAndStoresItInTheLibrary(t *testing.T) {
	s, r, u := service()
	stored, err := s.Compose(context.Background(), "ws1", layout())
	if err != nil {
		t.Fatal(err)
	}
	if stored.ID != "new-media" || len(u.uploaded) != 1 || r.layout.Headline != "Todos os canais" ||
		r.images.Image != "https://cdn/photo.jpg" || r.images.Logo != "https://cdn/logo.png" {
		t.Fatalf("stored %+v rendered %+v %+v", stored, r.layout, r.images)
	}
}

func TestComposeWorksWithoutALogo(t *testing.T) {
	s, r, _ := service()
	l := layout()
	l.LogoMediaID = ""
	if _, err := s.Compose(context.Background(), "ws1", l); err != nil {
		t.Fatal(err)
	}
	if r.images.Image != "https://cdn/photo.jpg" || r.images.Logo != "" {
		t.Fatalf("images %+v", r.images)
	}
}

func TestComposeRefusesImagesItCannotUse(t *testing.T) {
	for name, id := range map[string]string{
		"another workspace": "other",
		"a video":           "clip",
		"missing":           "nope",
	} {
		s, _, u := service()
		l := layout()
		l.ImageMediaID = id
		var invalid *domain.ValidationError
		if _, err := s.Compose(context.Background(), "ws1", l); !errors.As(err, &invalid) || len(u.uploaded) != 0 {
			t.Fatalf("%s: err %v uploaded %v", name, err, u.uploaded)
		}
	}
}

func TestComposeNeverStoresAFailedRender(t *testing.T) {
	s, r, u := service()
	r.err = errors.New("chromium crashed")
	if _, err := s.Compose(context.Background(), "ws1", layout()); err == nil || len(u.uploaded) != 0 {
		t.Fatalf("err %v uploaded %v", err, u.uploaded)
	}
}
