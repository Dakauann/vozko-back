package media

type UploadMediaUseCase interface {
	UploadMedia(workspaceID string, image []byte, mediaName string, mediaType MediaType, description string) (Media, error)
}

type ListMediaUseCase interface {
	ListMedia(workspaceID string) ([]Media, error)
}

type GetMediaUseCase interface {
	GetMedia(workspaceID, mediaID string) (*Media, error)
}
