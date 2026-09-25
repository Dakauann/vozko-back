package media_usecase

import (
	"context"
	"fmt"

	"vozko/domain/media"
)

type readMediaUseCase struct {
	get   media.GetMediaUseCase
	files media.FileReader
}

func NewReadMediaUseCase(get media.GetMediaUseCase, files media.FileReader) media.ReadMediaUseCase {
	return &readMediaUseCase{get: get, files: files}
}

func (uc *readMediaUseCase) Read(ctx context.Context, workspaceID, mediaID string) (*media.Content, error) {
	found, err := uc.get.GetMedia(workspaceID, mediaID)
	if err != nil {
		return nil, err
	}
	key, ok := uc.files.KeyFromURL(found.URL)
	if !ok {
		return nil, media.ErrMediaNotFound
	}
	data, contentType, err := uc.files.DownloadFile(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("download media %s: %w", mediaID, err)
	}
	if len(data) > media.MaxReadBytes {
		return nil, media.ErrMediaTooLarge
	}
	return &media.Content{Media: found, Name: found.DisplayName(), ContentType: contentType, Data: data}, nil
}
