package imagerehost

import (
	"context"
	"fmt"
	"mime"
	"strings"

	"vozko/domain/media"
)

type Fetcher interface {
	FetchBytes(ctx context.Context, url string) ([]byte, string, error)
}

type Store struct {
	fetcher Fetcher
	storage media.FileStorage
}

func New(fetcher Fetcher, storage media.FileStorage) *Store {
	if fetcher == nil || storage == nil {
		panic("image rehost: the fetcher and the file storage are required")
	}
	return &Store{fetcher: fetcher, storage: storage}
}

func (s *Store) Rehost(ctx context.Context, keyBase, url string) (string, error) {
	data, contentType, err := s.fetcher.FetchBytes(ctx, url)
	if err != nil {
		return "", err
	}
	mediaType, _, _ := mime.ParseMediaType(contentType)
	if !strings.HasPrefix(mediaType, "image/") {
		return "", fmt.Errorf("image rehost: %q is not an image", contentType)
	}
	key := keyBase + Extension(mediaType)
	if err := s.storage.UploadFile(key, data, mediaType); err != nil {
		return "", err
	}
	return key, nil
}

func (s *Store) URL(key string) string {
	if key == "" {
		return ""
	}
	return s.storage.GetFileURL(key)
}

func Extension(mediaType string) string {
	switch mediaType {
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	}
	return ".jpg"
}
