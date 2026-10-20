package facebook

import (
	"context"

	"vozko/domain/media"
	"vozko/usecases/shared/imagerehost"
)

type BytesFetcher = imagerehost.Fetcher

type PictureStore struct {
	images *imagerehost.Store
}

func NewPictureStore(fetcher BytesFetcher, storage media.FileStorage) *PictureStore {
	return &PictureStore{images: imagerehost.New(fetcher, storage)}
}

func (s *PictureStore) StorePagePicture(ctx context.Context, pageID, url string) (string, error) {
	return s.images.Rehost(ctx, "facebook/pages/"+pageID+"/picture", url)
}

func (s *PictureStore) StoreContactAvatar(ctx context.Context, contactID, url string) (string, error) {
	return s.images.Rehost(ctx, "facebook/contacts/"+contactID+"/avatar", url)
}

func (s *PictureStore) URL(key string) string {
	return s.images.URL(key)
}
