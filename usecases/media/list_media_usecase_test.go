package media_usecase

import (
	"testing"

	"vozko/domain/media"
)

type workspaceMedias struct {
	media.MediaRepository
	stored []media.Media
}

func (r workspaceMedias) ListMediasByWorkspace(string) ([]media.Media, error) { return r.stored, nil }

func TestTheLibraryLeavesOutEditingProxies(t *testing.T) {
	uc := NewListMediaUseCase(workspaceMedias{stored: []media.Media{{ID: "clip", Type: media.MediaTypeProductVideo}, {ID: "proxy", Type: media.MediaTypeStudioProxy}}})
	listed, err := uc.ListMedia("ws")
	if err != nil || len(listed) != 1 || listed[0].ID != "clip" {
		t.Fatalf("listed %+v err %v", listed, err)
	}
}
