package tiktok

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

var errNotAnImage = errors.New("tiktok: the downloaded file is not an image")

type ImageStore struct {
	fetcher MediaFetcher
	objects ObjectStore
}

func NewImageStore(fetcher MediaFetcher, objects ObjectStore) *ImageStore {
	if fetcher == nil || objects == nil {
		panic("tiktok: the image store requires a media fetcher and an object store")
	}
	return &ImageStore{fetcher: fetcher, objects: objects}
}

func AccountAvatarKey(accountID string) string { return "tiktok/accounts/" + accountID + "/avatar" }

func ContactAvatarKey(accountID, contactID string) string {
	return "tiktok/accounts/" + accountID + "/contacts/" + contactID + "/avatar"
}

func VideoThumbnailKey(accountID, itemID string) string {
	return "tiktok/accounts/" + accountID + "/videos/" + itemID + "/thumbnail"
}

func (s *ImageStore) Rehost(ctx context.Context, key, sourceURL string) (string, error) {
	sourceURL = strings.TrimSpace(sourceURL)
	if sourceURL == "" {
		return "", nil
	}
	data, err := s.fetcher.Fetch(ctx, sourceURL)
	if err != nil {
		return "", err
	}
	mimeType := http.DetectContentType(data)
	if !strings.HasPrefix(mimeType, "image/") {
		return "", errNotAnImage
	}
	if err := s.objects.UploadFile(key, data, mimeType); err != nil {
		return "", err
	}
	return key, nil
}

func (s *ImageStore) URL(key string) string {
	if strings.TrimSpace(key) == "" {
		return ""
	}
	return s.objects.GetFileURL(key)
}
