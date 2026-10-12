package studio_usecase

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"github.com/google/uuid"

	"vozko/domain/media"
	"vozko/domain/studio/library"
	media_usecase "vozko/usecases/media"
)

const libraryAssetRows = 16

var (
	errLibraryAssetRows   = errors.New("studio: the library picture could not get a row in the workspace library")
	libraryAssetNamespace = uuid.MustParse("5d7b8f0e-3c41-4b8a-9e2f-6a1d0c9b8e47")
)

type LibraryCatalog interface {
	Find(id string) (library.Item, bool)
}

type LibraryAssetMedia interface {
	RegisterStored(workspaceID, id, key string, mediaType media.MediaType, description string) (media.Media, error)
	GetMedia(workspaceID, mediaID string) (*media.Media, error)
}

type LibraryAssetService struct {
	catalog LibraryCatalog
	storage media.StreamStorage
	media   LibraryAssetMedia
}

func NewLibraryAssetService(catalog LibraryCatalog, storage media.StreamStorage, assets LibraryAssetMedia) (*LibraryAssetService, error) {
	if catalog == nil || storage == nil || assets == nil {
		return nil, ErrMissingDependency
	}
	return &LibraryAssetService{catalog: catalog, storage: storage, media: assets}, nil
}

func (s *LibraryAssetService) Import(ctx context.Context, workspaceID, itemID, locale string, file io.Reader) (media.Media, error) {
	item, ok := s.catalog.Find(itemID)
	if !ok || item.Kind != library.KindAsset || item.Asset == nil {
		return media.Media{}, library.ErrUnknownAsset
	}
	raw, err := io.ReadAll(io.LimitReader(file, library.MaxAssetBytes+1))
	if err != nil {
		return media.Media{}, err
	}
	sum := sha256.Sum256(raw)
	if len(raw) > library.MaxAssetBytes || hex.EncodeToString(sum[:]) != item.Asset.SHA256 {
		return media.Media{}, library.ErrAssetMismatch
	}
	for row := 0; row < libraryAssetRows; row++ {
		id := uuid.NewSHA1(libraryAssetNamespace, []byte(fmt.Sprintf("%s/%s/%d", workspaceID, item.ID, row))).String()
		found, err := s.media.GetMedia(workspaceID, id)
		if err == nil {
			return *found, nil
		}
		if !errors.Is(err, media.ErrMediaNotFound) {
			return media.Media{}, err
		}
		saved, taken, err := s.register(ctx, workspaceID, id, item, locale, raw)
		if !taken {
			return saved, err
		}
	}
	return media.Media{}, errLibraryAssetRows
}

func (s *LibraryAssetService) register(ctx context.Context, workspaceID, id string, item library.Item, locale string, raw []byte) (media.Media, bool, error) {
	key := fmt.Sprintf("studio/library/%s/%s.webp", workspaceID, id)
	if err := s.storage.PutStream(ctx, key, item.Asset.ContentType, bytes.NewReader(raw)); err != nil {
		return media.Media{}, false, err
	}
	saved, err := s.media.RegisterStored(workspaceID, id, key, media.MediaTypeProductImage, item.Names.In(locale))
	if err == nil {
		return saved, false, nil
	}
	if errors.Is(err, media_usecase.ErrUploadLimit) {
		return media.Media{}, false, errors.Join(err, s.storage.DeleteFile(ctx, key))
	}
	found, lookup := s.media.GetMedia(workspaceID, id)
	if lookup == nil {
		return *found, false, nil
	}
	cleanup := s.storage.DeleteFile(ctx, key)
	if errors.Is(lookup, media.ErrMediaNotFound) {
		return media.Media{}, true, nil
	}
	return media.Media{}, false, errors.Join(err, lookup, cleanup)
}
