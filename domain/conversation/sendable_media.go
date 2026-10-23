package conversation

import (
	"errors"
	"fmt"
	"strings"

	"vozko/domain/media"
	"vozko/domain/shared"
)

const MediaNotSendableCode = "media_not_sendable"

var (
	ErrMediaNotSendable    = fmt.Errorf("conversation: this media cannot be sent in this conversation: %w", media.ErrMediaNotFound)
	ErrMediaPurposeInvalid = errors.New("conversation: media purpose is invalid")
)

type MediaPurpose string

const (
	MediaPurposeMessage   MediaPurpose = "message"
	MediaPurposeNote      MediaPurpose = "note"
	MediaPurposeEmailBody MediaPurpose = "email_body"
)

func (p MediaPurpose) Valid() bool {
	return p == MediaPurposeMessage || p == MediaPurposeNote || p == MediaPurposeEmailBody
}

type EntryMediaReader interface {
	GetInEntry(entryID string, entryType shared.EntryType, id string) (*ConversationMedia, error)
}

type LibraryMediaReader interface {
	GetMediaForWorkspace(workspaceID, mediaID string) (*media.Media, error)
}

func (m *ConversationMedia) SendableIn(entryID string, entryType shared.EntryType) bool {
	return m != nil &&
		strings.TrimSpace(entryID) != "" &&
		m.BelongsTo(strings.TrimSpace(entryID), entryType) &&
		m.Purpose == MediaPurposeMessage &&
		strings.TrimSpace(m.URL) != ""
}

func SendableUpload(reader EntryMediaReader, entryID string, entryType shared.EntryType, mediaID string) (*ConversationMedia, error) {
	mediaID = strings.TrimSpace(mediaID)
	if reader == nil || mediaID == "" {
		return nil, mediaNotSendable()
	}
	found, err := reader.GetInEntry(strings.TrimSpace(entryID), entryType, mediaID)
	if err != nil || !found.SendableIn(entryID, entryType) {
		return nil, mediaNotSendable()
	}
	return found, nil
}

func SendableLibraryMedia(reader LibraryMediaReader, workspaceID, mediaID string) (*media.Media, error) {
	mediaID = strings.TrimSpace(mediaID)
	workspaceID = strings.TrimSpace(workspaceID)
	if reader == nil || mediaID == "" || workspaceID == "" {
		return nil, mediaNotSendable()
	}
	found, err := reader.GetMediaForWorkspace(workspaceID, mediaID)
	if err != nil || !found.SendableFor(workspaceID) {
		return nil, mediaNotSendable()
	}
	return found, nil
}

func mediaNotSendable() error {
	return &SendRefusal{Code: MediaNotSendableCode, Err: ErrMediaNotSendable}
}
