package media_usecase

import (
	"vozko/domain/media"
)

type getMediaUseCase struct {
	repo media.MediaRepository
}

func NewGetMediaUseCase(repo media.MediaRepository) media.GetMediaUseCase {
	return &getMediaUseCase{repo: repo}
}

func (uc *getMediaUseCase) GetMedia(mediaID string) (*media.Media, error) {
	return uc.repo.GetMediaByID(mediaID)
}
