package media_usecase_test

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/media"
	wsc "vozko/domain/workspace_config"
	media_usecase "vozko/usecases/media"
)

type fakeConfigRepo struct {
	cfg     *wsc.WorkspaceConfig
	upserts []*wsc.WorkspaceConfig
}

func (f *fakeConfigRepo) GetByWorkspaceID(_ context.Context, ws string) (*wsc.WorkspaceConfig, error) {
	if f.cfg != nil {
		return f.cfg, nil
	}
	return &wsc.WorkspaceConfig{WorkspaceID: ws}, nil
}
func (f *fakeConfigRepo) Upsert(_ context.Context, cfg *wsc.WorkspaceConfig) error {
	cp := *cfg
	f.upserts = append(f.upserts, &cp)
	return nil
}
func (f *fakeConfigRepo) EnsureExists(context.Context, string) error { return nil }

func TestDeleteHoldMusic_DeletesAndClearsSelection(t *testing.T) {
	t.Parallel()
	repo := &fakeMediaRepo{byID: map[string]*media.Media{
		"m-1": {ID: "m-1", WorkspaceID: "ws-1", Type: media.MediaTypeHoldMusic},
	}}
	configs := &fakeConfigRepo{cfg: &wsc.WorkspaceConfig{WorkspaceID: "ws-1", HoldMusicTrack: "m-1"}}
	uc := media_usecase.NewDeleteHoldMusicUseCase(repo, configs)

	if err := uc.DeleteHoldMusic(context.Background(), "ws-1", "m-1"); err != nil {
		t.Fatalf("DeleteHoldMusic: %v", err)
	}
	if len(repo.deleted) != 1 || repo.deleted[0] != "m-1" {
		t.Fatalf("deleted = %v", repo.deleted)
	}
	if len(configs.upserts) != 1 || configs.upserts[0].HoldMusicTrack != "" {
		t.Fatalf("deleting the SELECTED track must reset the selection, upserts=%v", configs.upserts)
	}
}

func TestDeleteHoldMusic_KeepsUnrelatedSelection(t *testing.T) {
	t.Parallel()
	repo := &fakeMediaRepo{byID: map[string]*media.Media{
		"m-2": {ID: "m-2", WorkspaceID: "ws-1", Type: media.MediaTypeHoldMusic},
	}}
	configs := &fakeConfigRepo{cfg: &wsc.WorkspaceConfig{WorkspaceID: "ws-1", HoldMusicTrack: "builtin:lofi"}}
	uc := media_usecase.NewDeleteHoldMusicUseCase(repo, configs)

	if err := uc.DeleteHoldMusic(context.Background(), "ws-1", "m-2"); err != nil {
		t.Fatalf("DeleteHoldMusic: %v", err)
	}
	if len(configs.upserts) != 0 {
		t.Fatalf("an unrelated selection must not be touched, upserts=%v", configs.upserts)
	}
}

func TestDeleteHoldMusic_CrossWorkspaceLooksLikeMissing(t *testing.T) {
	t.Parallel()
	repo := &fakeMediaRepo{byID: map[string]*media.Media{
		"m-1": {ID: "m-1", WorkspaceID: "ws-OTHER", Type: media.MediaTypeHoldMusic},
	}}
	uc := media_usecase.NewDeleteHoldMusicUseCase(repo, &fakeConfigRepo{})
	err := uc.DeleteHoldMusic(context.Background(), "ws-1", "m-1")
	if !errors.Is(err, media.ErrMediaNotFound) {
		t.Fatalf("cross-workspace delete: err=%v, want ErrMediaNotFound (no existence leak)", err)
	}
	if len(repo.deleted) != 0 {
		t.Fatal("nothing may be deleted across workspaces")
	}
}

func TestDeleteHoldMusic_RefusesOtherMediaTypes(t *testing.T) {
	t.Parallel()
	repo := &fakeMediaRepo{byID: map[string]*media.Media{
		"m-1": {ID: "m-1", WorkspaceID: "ws-1", Type: media.MediaTypeAudio},
	}}
	uc := media_usecase.NewDeleteHoldMusicUseCase(repo, &fakeConfigRepo{})
	err := uc.DeleteHoldMusic(context.Background(), "ws-1", "m-1")
	if !errors.Is(err, media.ErrNotHoldMusic) {
		t.Fatalf("err=%v, want ErrNotHoldMusic (workflow-referenced media must not be deletable here)", err)
	}
}
