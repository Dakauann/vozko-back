package leadimportfiles

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"vozko/domain/leadimport"
	"vozko/domain/media"
)

const (
	keyPrefix   = "lead-imports/"
	contentType = "text/csv; charset=utf-8"
	description = "Planilha de importação de leads"
)

type Objects interface {
	UploadFile(key string, data []byte, contentType string) error
	GetFileURL(key string) string
	KeyFromURL(url string) (string, bool)
	DeleteFile(ctx context.Context, key string) error
}

type Library interface {
	CreateMedia(m *media.Media) error
	GetMediaByID(id string) (*media.Media, error)
	DeleteMedia(id string) error
}

type Files struct {
	objects Objects
	library Library
	reader  media.ReadMediaUseCase
	now     func() time.Time
}

var _ leadimport.Files = (*Files)(nil)

func New(objects Objects, library Library, reader media.ReadMediaUseCase) (*Files, error) {
	if objects == nil || library == nil || reader == nil {
		return nil, errors.New("lead import files: object storage, the media library and the media reader are required")
	}
	return &Files{objects: objects, library: library, reader: reader, now: time.Now}, nil
}

func (f *Files) Store(_ context.Context, workspaceID, _ string, data []byte) (string, error) {
	key := keyPrefix + workspaceID + "/" + uuid.NewString() + ".csv"
	if err := f.objects.UploadFile(key, data, contentType); err != nil {
		return "", fmt.Errorf("lead import files: upload: %w", err)
	}
	record := &media.Media{WorkspaceID: workspaceID, URL: f.objects.GetFileURL(key), CreatedAt: f.now(), Type: media.MediaTypeLeadImport, Description: description}
	if err := f.library.CreateMedia(record); err != nil {
		return "", fmt.Errorf("lead import files: record: %w", err)
	}
	return record.ID, nil
}

func (f *Files) Read(ctx context.Context, workspaceID, mediaID string) ([]byte, error) {
	content, err := f.content(ctx, workspaceID, mediaID)
	if err != nil {
		return nil, err
	}
	return content.Data, nil
}

func (f *Files) ReadLibrary(ctx context.Context, workspaceID, mediaID string) ([]byte, error) {
	content, err := f.content(ctx, workspaceID, mediaID)
	if err != nil {
		return nil, err
	}
	if content.Media == nil || !content.Media.Type.Listed() {
		return nil, leadimport.ErrFileUnavailable
	}
	return content.Data, nil
}

func (f *Files) content(ctx context.Context, workspaceID, mediaID string) (*media.Content, error) {
	content, err := f.reader.Read(ctx, workspaceID, mediaID)
	switch {
	case errors.Is(err, media.ErrMediaNotFound):
		return nil, leadimport.ErrFileUnavailable
	case errors.Is(err, media.ErrMediaTooLarge):
		return nil, leadimport.ErrFileTooLarge
	case err != nil:
		return nil, err
	case content == nil:
		return nil, leadimport.ErrFileUnavailable
	}
	return content, nil
}

func (f *Files) Erase(ctx context.Context, workspaceID, mediaID string) error {
	stored, err := f.library.GetMediaByID(mediaID)
	if errors.Is(err, media.ErrMediaNotFound) || (err == nil && stored == nil) {
		return nil
	}
	if err != nil {
		return err
	}
	if stored.WorkspaceID != workspaceID || stored.Type != media.MediaTypeLeadImport {
		return leadimport.ErrFileUnavailable
	}
	if key, ok := f.objects.KeyFromURL(stored.URL); ok {
		if err := f.objects.DeleteFile(ctx, key); err != nil {
			return fmt.Errorf("lead import files: delete object: %w", err)
		}
	}
	return f.library.DeleteMedia(mediaID)
}
