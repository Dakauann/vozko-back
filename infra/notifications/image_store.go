package notification_service

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"vozko/domain/media"
	"vozko/domain/notification"
)

const notificationImagePrefix = "notifications/images/"

type ImageStore struct {
	storage media.FileStorage
}

var _ notification.ImageStore = (*ImageStore)(nil)

func NewImageStore(storage media.FileStorage) *ImageStore {
	return &ImageStore{storage: storage}
}

func (s *ImageStore) Save(_ context.Context, data []byte, img notification.Image) (string, error) {
	if s.storage == nil {
		return "", errors.New("notification images: file storage is not configured")
	}
	key := notificationImagePrefix + uuid.NewString() + img.Ext
	if err := s.storage.UploadFile(key, data, img.MIME); err != nil {
		return "", err
	}
	return s.storage.GetFileURL(key), nil
}

func (s *ImageStore) Owns(url string) bool {
	if s.storage == nil {
		return false
	}
	base := s.storage.GetFileURL(notificationImagePrefix)
	if !strings.HasPrefix(base, "https://") || !strings.HasPrefix(url, base) {
		return false
	}
	rest := strings.TrimPrefix(url, base)
	return rest != "" && !strings.ContainsAny(rest, "/?#\\")
}
