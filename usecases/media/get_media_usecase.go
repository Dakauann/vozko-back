package media_usecase

import (
	"slices"
	"strings"

	"vozko/domain/media"
)

type getMediaUseCase struct {
	repo    media.MediaRepository
	private []media.MediaType
}

func NewGetMediaUseCase(repo media.MediaRepository, private ...media.MediaType) media.GetMediaUseCase {
	return &getMediaUseCase{repo: repo, private: private}
}

func (uc *getMediaUseCase) GetMedia(workspaceID, mediaID string) (*media.Media, error) {
	if strings.TrimSpace(workspaceID) == "" || uc.repo == nil {
		return nil, media.ErrMediaNotFound
	}
	found, err := uc.repo.GetMediaByID(mediaID)
	if err != nil {
		return nil, err
	}
	if found == nil || found.WorkspaceID != workspaceID || !uc.readable(found.Type) {
		return nil, media.ErrMediaNotFound
	}
	return found, nil
}

func (uc *getMediaUseCase) readable(kind media.MediaType) bool {
	return !kind.Private() || slices.Contains(uc.private, kind)
}
