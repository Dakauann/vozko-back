package media_usecase

import (
	"vozko/domain/media"
)

type ListMediaUseCase struct {
	mediaRepository media.MediaRepository
}

func NewListMediaUseCase(mediaRepository media.MediaRepository) media.ListMediaUseCase {
	return &ListMediaUseCase{
		mediaRepository: mediaRepository,
	}
}

func (uc *ListMediaUseCase) ListMedia(workspaceID string) ([]media.Media, error) {
	images, err := uc.mediaRepository.ListMediasByWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	return images, nil
}
