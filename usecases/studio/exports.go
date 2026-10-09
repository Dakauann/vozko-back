package studio_usecase

import (
	"context"
	"errors"
	"io"

	"github.com/google/uuid"

	"vozko/domain/media"
	"vozko/domain/studio"
)

type ExportLibrary interface {
	RegisterStored(workspaceID, id, key string, mediaType media.MediaType, description string) (media.Media, error)
}

type ExportService struct {
	projects studio.Repository
	storage  media.StreamStorage
	library  ExportLibrary
	newID    func() string
}

func NewExportService(projects studio.Repository, storage media.StreamStorage, library ExportLibrary) (*ExportService, error) {
	if projects == nil || storage == nil || library == nil {
		return nil, ErrMissingDependency
	}
	return &ExportService{projects: projects, storage: storage, library: library, newID: uuid.NewString}, nil
}

func (s *ExportService) Save(ctx context.Context, workspaceID, projectID string, video io.Reader) (media.Media, error) {
	project, err := s.projects.Get(ctx, workspaceID, projectID)
	if err != nil {
		return media.Media{}, err
	}
	if project.Kind != studio.KindVideo {
		return media.Media{}, studio.ErrNotVideo
	}
	id := s.newID()
	key := studio.ExportKey(workspaceID, project.ID, id)
	if err := s.storage.PutStream(ctx, key, studio.ExportContentType, studio.NewExportReader(video)); err != nil {
		return media.Media{}, err
	}
	saved, err := s.library.RegisterStored(workspaceID, id, key, media.MediaTypeProductVideo, project.Name)
	if err != nil {
		return media.Media{}, errors.Join(err, s.storage.DeleteFile(ctx, key))
	}
	return saved, nil
}
