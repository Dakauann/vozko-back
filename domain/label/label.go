package label

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"vozko/domain/shared"
)

type Label struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspaceId"`
	Name        string    `json:"name"`
	Color       string    `json:"color,omitempty"`
	Position    int       `json:"position"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type EntryLabel struct {
	ID          string    `json:"id"`
	LabelID     string    `json:"labelId"`
	EntryID     string    `json:"entryId"`
	EntryType   string    `json:"entryType"`
	WorkspaceID string    `json:"workspaceId"`
	CreatedAt   time.Time `json:"createdAt"`

	LabelName  string `json:"labelName,omitempty"`
	LabelColor string `json:"labelColor,omitempty"`
}

var (
	ErrLabelNotFound      = errors.New("label not found")
	ErrLabelNameRequired  = errors.New("label name is required")
	ErrLabelNameExists    = errors.New("label with this name already exists in this workspace")
	ErrEntryLabelNotFound = errors.New("entry label assignment not found")
	ErrEntryLabelExists   = errors.New("this label is already assigned to this entry")
	ErrEntryNotFound      = errors.New("entry not found")
	ErrInvalidEntryType   = fmt.Errorf("entry_type must be %s", shared.FormatEntryTypes(shared.CRMTaggableEntryTypes()))
	ErrUnauthorized       = errors.New("unauthorized access to this label")
)

func (l *Label) Normalize() {
	l.Name = strings.TrimSpace(strings.ToLower(l.Name))
	l.Color = strings.TrimSpace(l.Color)
}

func (l *Label) Validate() error {
	if strings.TrimSpace(l.Name) == "" {
		return ErrLabelNameRequired
	}
	return nil
}

// ValidateEntryType gates which conversations can be staged and labelled.
//
// The set lives in domain/shared so this and the label/stage counterpart cannot
// drift apart, and so adding a channel does not mean hunting for hardcoded
// entry-type lists. Instagram was rejected here while its cards already rendered
// on the board.
func ValidateEntryType(entryType string) error {
	if !shared.EntryType(entryType).SupportsCRMTagging() {
		return ErrInvalidEntryType
	}
	return nil
}

type LabelGroup struct {
	ID          string           `json:"id"`
	WorkspaceID string           `json:"workspaceId"`
	Name        string           `json:"name"`
	Items       []LabelGroupItem `json:"items"`
}

type LabelGroupItem struct {
	ID           string `json:"id"`
	LabelGroupID string `json:"labelGroupId"`
	Name         string `json:"name"`
	Color        string `json:"color"`
	Position     int    `json:"position"`
}
