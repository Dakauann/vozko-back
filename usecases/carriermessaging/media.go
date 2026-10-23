package carriermessaging_usecase

import (
	"context"
	"fmt"
	"mime"
	"strings"

	"vozko/domain/carriermessaging"
	"vozko/domain/conversation"
	"vozko/domain/media"
	"vozko/domain/rcs"
)

type MediaLibrary interface {
	GetMediaForWorkspace(workspaceID, mediaID string) (*media.Media, error)
}

type RemoteMedia interface {
	Describe(ctx context.Context, url string) (contentType string, size int64, err error)
}

func (s *Service) ResolveMedia(ctx context.Context, workspaceID, mediaID string) (*rcs.Media, error) {
	item, err := conversation.SendableLibraryMedia(s.Library, workspaceID, mediaID)
	if err != nil {
		return nil, err
	}
	contentType, size, err := s.RemoteMedia.Describe(ctx, item.URL)
	if err != nil {
		return nil, err
	}
	mediaType, _, err := mime.ParseMediaType(strings.TrimSpace(contentType))
	if err != nil {
		return nil, fmt.Errorf("%w: unreadable type %q", rcs.ErrMediaInvalid, contentType)
	}
	kind, ok := rcs.MediaKindFor(mediaType)
	if !ok {
		return nil, fmt.Errorf("%w: %q", rcs.ErrMediaInvalid, mediaType)
	}
	resolved := rcs.Media{Kind: kind, URL: item.URL, MIMEType: mediaType, Name: item.Description, SizeBytes: size}
	if err := (rcs.Content{Media: &resolved}).Validate(); err != nil {
		return nil, err
	}
	return &resolved, nil
}

func (s *Service) sendableContentMedia(workspaceID string, conv carriermessaging.Conversation, content rcs.Content) error {
	scope := conversation.MediaSendScope{WorkspaceID: workspaceID, EntryID: conv.ID, EntryType: conv.Channel.EntryType()}
	for _, url := range content.MediaURLs() {
		if err := s.MediaURLs.SendableURL(scope, url); err != nil {
			return err
		}
	}
	return nil
}
