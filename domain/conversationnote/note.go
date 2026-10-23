package conversationnote

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"vozko/domain/shared"
)

const (
	MaxBodyRunes    = 4000
	MaxMentions     = 20
	DefaultPageSize = 50
	MaxPageSize     = 100
)

var (
	ErrIDRequired        = errors.New("conversationnote: a note needs its id")
	ErrWorkspaceRequired = errors.New("conversationnote: a note needs its workspace")
	ErrEntryRequired     = errors.New("conversationnote: a note needs a conversation")
	ErrAuthorRequired    = errors.New("conversationnote: a note needs its author")
	ErrBodyRequired      = errors.New("conversationnote: the note is empty")
	ErrBodyTooLong       = errors.New("conversationnote: a note can have at most 4000 characters")
	ErrTooManyMentions   = errors.New("conversationnote: a note can mention at most 20 people")
	ErrNotAuthor         = errors.New("conversationnote: only the author can delete a note")
	ErrNotFound          = errors.New("conversationnote: note not found")
)

type Note struct {
	ID          string
	WorkspaceID string
	EntryType   shared.EntryType
	EntryID     string
	AuthorID    string
	Body        string
	Mentions    []string
	ImageIDs    []string
	CreatedAt   time.Time
	DeletedAt   *time.Time
}

type Draft struct {
	WorkspaceID string
	Entry       shared.EntryRef
	AuthorID    string
	Body        string
	Mentions    []string
	ImageIDs    []string
}

func New(d Draft, id string, now time.Time) (Note, error) {
	note := Note{
		ID:          strings.TrimSpace(id),
		WorkspaceID: strings.TrimSpace(d.WorkspaceID),
		EntryType:   d.Entry.EntryType,
		EntryID:     strings.TrimSpace(d.Entry.EntryID),
		AuthorID:    strings.TrimSpace(d.AuthorID),
		Body:        strings.TrimSpace(d.Body),
		CreatedAt:   now.UTC(),
	}
	if err := note.validateIdentity(); err != nil {
		return Note{}, err
	}
	images, err := NormalizeImageIDs(d.ImageIDs)
	if err != nil {
		return Note{}, err
	}
	if err := validateBody(note.Body, len(images) > 0); err != nil {
		return Note{}, err
	}
	mentions, err := NormalizeMentions(d.Mentions)
	if err != nil {
		return Note{}, err
	}
	note.Mentions = mentions
	note.ImageIDs = images
	return note, nil
}

func (n Note) validateIdentity() error {
	switch {
	case n.ID == "":
		return ErrIDRequired
	case n.WorkspaceID == "":
		return ErrWorkspaceRequired
	case n.EntryID == "" || !n.EntryType.SupportsConversationView():
		return ErrEntryRequired
	case n.AuthorID == "":
		return ErrAuthorRequired
	}
	return nil
}

func validateBody(body string, hasImages bool) error {
	if body == "" && !hasImages {
		return ErrBodyRequired
	}
	if utf8.RuneCountInString(body) > MaxBodyRunes {
		return ErrBodyTooLong
	}
	return nil
}

func NormalizeMentions(ids []string) ([]string, error) {
	out := shared.DistinctTrimmed(ids)
	if len(out) > MaxMentions {
		return nil, ErrTooManyMentions
	}
	return out, nil
}

func (n Note) Entry() shared.EntryRef {
	return shared.EntryRef{EntryID: n.EntryID, EntryType: n.EntryType}
}

func (n Note) DeletableBy(userID string) error {
	if n.DeletedAt != nil {
		return ErrNotFound
	}
	if strings.TrimSpace(userID) == "" || userID != n.AuthorID {
		return ErrNotAuthor
	}
	return nil
}

func PageLimit(requested int) int {
	if requested <= 0 {
		return DefaultPageSize
	}
	if requested > MaxPageSize {
		return MaxPageSize
	}
	return requested
}
