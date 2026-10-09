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
	stored, err := uc.mediaRepository.ListMediasByWorkspace(workspaceID)
	if err != nil {
		return nil, err
	}
	listed := make([]media.Media, 0, len(stored))
	for _, m := range stored {
		if m.Type.Listed() {
			listed = append(listed, m)
		}
	}
	return listed, nil
}
