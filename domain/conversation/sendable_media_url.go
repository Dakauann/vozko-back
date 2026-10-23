package conversation

import (
	"strings"

	"vozko/domain/media"
	"vozko/domain/shared"
)

type MediaStorageHosts interface {
	OwnsURL(rawURL string) bool
}

type LibraryMediaURLReader interface {
	GetMediaForWorkspaceByURL(workspaceID, url string) (*media.Media, error)
}

type EntryMediaURLReader interface {
	GetInEntryByURL(entryID string, entryType shared.EntryType, url string) (*ConversationMedia, error)
}

type MediaURLResolver struct {
	Storage MediaStorageHosts
	Library LibraryMediaURLReader
	Uploads EntryMediaURLReader
}

type MediaSendScope struct {
	WorkspaceID string
	EntryID     string
	EntryType   shared.EntryType
}

func (r *MediaURLResolver) Owns(rawURL string) bool {
	return r != nil && r.Storage != nil && r.Storage.OwnsURL(strings.TrimSpace(rawURL))
}

func (r *MediaURLResolver) SendableURL(scope MediaSendScope, rawURL string) error {
	rawURL = strings.TrimSpace(rawURL)
	if r == nil || r.Storage == nil || rawURL == "" {
		return mediaNotSendable()
	}
	if !r.Storage.OwnsURL(rawURL) {
		return nil
	}
	if r.libraryAllows(scope, rawURL) || r.uploadAllows(scope, rawURL) {
		return nil
	}
	return mediaNotSendable()
}

func (r *MediaURLResolver) libraryAllows(scope MediaSendScope, rawURL string) bool {
	workspaceID := strings.TrimSpace(scope.WorkspaceID)
	if r.Library == nil || workspaceID == "" {
		return false
	}
	found, err := r.Library.GetMediaForWorkspaceByURL(workspaceID, rawURL)
	return err == nil && found.SendableFor(workspaceID) && strings.TrimSpace(found.URL) == rawURL
}

func (r *MediaURLResolver) uploadAllows(scope MediaSendScope, rawURL string) bool {
	entryID := strings.TrimSpace(scope.EntryID)
	if r.Uploads == nil || entryID == "" {
		return false
	}
	found, err := r.Uploads.GetInEntryByURL(entryID, scope.EntryType, rawURL)
	return err == nil && found.SendableIn(entryID, scope.EntryType) && strings.TrimSpace(found.URL) == rawURL
}
