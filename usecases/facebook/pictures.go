package facebook

import (
	"context"
	"fmt"
	"mime"
	"strings"

	"vozko/domain/media"
)

type BytesFetcher interface {
	FetchBytes(ctx context.Context, url string) ([]byte, string, error)
}

type PictureStore struct {
	fetcher BytesFetcher
	storage media.FileStorage
}

func NewPictureStore(fetcher BytesFetcher, storage media.FileStorage) *PictureStore {
	return &PictureStore{fetcher: fetcher, storage: storage}
}

func (s *PictureStore) StorePagePicture(ctx context.Context, pageID, url string) (string, error) {
	return s.store(ctx, "facebook/pages/"+pageID+"/picture", url)
}

func (s *PictureStore) StoreContactAvatar(ctx context.Context, contactID, url string) (string, error) {
	return s.store(ctx, "facebook/contacts/"+contactID+"/avatar", url)
}

func (s *PictureStore) URL(key string) string {
	if key == "" {
		return ""
	}
	return s.storage.GetFileURL(key)
}

func (s *PictureStore) store(ctx context.Context, keyBase, url string) (string, error) {
	data, contentType, err := s.fetcher.FetchBytes(ctx, url)
	if err != nil {
		return "", err
	}
	mediaType, _, _ := mime.ParseMediaType(contentType)
	if !strings.HasPrefix(mediaType, "image/") {
		return "", fmt.Errorf("facebook: picture is %q, not an image", contentType)
	}
	key := keyBase + imageExtension(mediaType)
	if err := s.storage.UploadFile(key, data, mediaType); err != nil {
		return "", err
	}
	return key, nil
}

func imageExtension(mediaType string) string {
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
