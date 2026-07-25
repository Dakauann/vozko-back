package media

type MediaRepository interface {
	CreateMedia(media *Media) error
	ListMediasByWorkspace(workspaceID string) ([]Media, error)
	DeleteMedia(mediaID string) error
	CountWorkspaceUploadsToday(workspaceID string) (int64, error)
	GetMediaByID(mediaID string) (*Media, error)
	GetMediasByIDs(mediaIDs []string) ([]Media, error)
	MediaExists(mediaID string) (bool, error)
	CountByWorkspaceID(workspaceID string) (int64, error)
	// CountByWorkspaceIDAndType counts a workspace's media of one type (the hold
	// music quota reads hold_music rows).
	CountByWorkspaceIDAndType(workspaceID string, mediaType MediaType) (int64, error)
}
