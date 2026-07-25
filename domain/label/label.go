package label

import (
	"errors"
	"strings"
	"time"
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
	ErrInvalidEntryType   = errors.New("entry_type must be 'voice', 'whatsapp', or 'support'")
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

func ValidateEntryType(entryType string) error {
	if entryType != "voice" && entryType != "whatsapp" && entryType != "support" {
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
