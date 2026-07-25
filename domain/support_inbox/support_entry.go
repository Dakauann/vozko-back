package support_inbox

import (
	"errors"
	"strings"
	"time"

	"vozko/domain/shared"
)

var (
	ErrEntryNotFound = errors.New("support entry not found")
)

type SupportEntry struct {
	ID           string    `json:"id"`
	InboxID      string    `json:"inboxId"`
	ContactName  string    `json:"contactName,omitempty"`
	ContactEmail string    `json:"contactEmail,omitempty"`
	SourceURL    string    `json:"sourceUrl,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

func (e *SupportEntry) Normalize() {
	e.ContactName = strings.TrimSpace(e.ContactName)
	e.ContactEmail = strings.TrimSpace(e.ContactEmail)
	e.SourceURL = strings.TrimSpace(e.SourceURL)
}

type EntryRepository interface {
	Create(entry *SupportEntry) error
	FindByID(id string) (*SupportEntry, error)
	Delete(id string) error
	List(input ListEntriesInput) (*shared.PaginatedResult[*SupportEntry], error)
	CountByInboxID(inboxID string) (int64, error)
}

type ListEntriesInput struct {
	InboxID string
	Options shared.QueryOptions
}
