package media_usecase

import (
	"strings"

	"vozko/domain/media"
)

type getMediaUseCase struct {
	repo media.MediaRepository
}

func NewGetMediaUseCase(repo media.MediaRepository) media.GetMediaUseCase {
	return &getMediaUseCase{repo: repo}
}

func (uc *getMediaUseCase) GetMedia(workspaceID, mediaID string) (*media.Media, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, media.ErrMediaNotFound
	}
	found, err := uc.repo.GetMediaByID(mediaID)
	if err != nil {
		return nil, err
	}
	if found == nil || found.WorkspaceID != workspaceID {
		return nil, media.ErrMediaNotFound
	}
	return found, nil
}
