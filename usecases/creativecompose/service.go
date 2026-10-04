package creativecompose

import (
	"context"
	"errors"
	"fmt"

	domain "vozko/domain/creativecompose"
	"vozko/domain/media"
)

type MediaLookup interface {
	GetMedia(workspaceID, mediaID string) (*media.Media, error)
}

type MediaUploader interface {
	UploadMedia(workspaceID string, data []byte, mediaName string, mediaType media.MediaType, description string) (media.Media, error)
}

type Service struct {
	renderer domain.Renderer
	media    MediaLookup
	uploader MediaUploader
}

func NewService(renderer domain.Renderer, lookup MediaLookup, uploader MediaUploader) *Service {
	return &Service{renderer: renderer, media: lookup, uploader: uploader}
}

func (s *Service) Compose(ctx context.Context, workspaceID string, layout domain.Layout) (*media.Media, error) {
	layout.Normalize()
	if err := layout.Validate(); err != nil {
		return nil, err
	}
	image, err := s.imageURL(workspaceID, layout.ImageMediaID, domain.FieldImage)
	if err != nil {
		return nil, err
	}
	logo, err := s.imageURL(workspaceID, layout.LogoMediaID, domain.FieldLogo)
	if err != nil {
		return nil, err
	}
	rendered, err := s.renderer.Render(ctx, layout, domain.Images{Image: image, Logo: logo})
	if err != nil {
		return nil, fmt.Errorf("creativecompose: render: %w", err)
	}
	stored, err := s.uploader.UploadMedia(workspaceID, rendered, "criativo-"+string(layout.Template)+".png", media.MediaTypeProductImage, layout.Headline)
	if err != nil {
		return nil, fmt.Errorf("creativecompose: store: %w", err)
	}
	return &stored, nil
}

func (s *Service) imageURL(workspaceID, mediaID, field string) (string, error) {
	if mediaID == "" {
		return "", nil
	}
	found, err := s.media.GetMedia(workspaceID, mediaID)
	if errors.Is(err, media.ErrMediaNotFound) || (err == nil && (found == nil || found.Type != media.MediaTypeProductImage || found.URL == "")) {
		return "", issue(field)
	}
	if err != nil {
		return "", fmt.Errorf("creativecompose: load %s: %w", mediaID, err)
	}
	return found.URL, nil
}

func issue(field string) error {
	return &domain.ValidationError{Issues: []domain.FieldIssue{{Field: field, Code: domain.CodeNotFound}}}
}
