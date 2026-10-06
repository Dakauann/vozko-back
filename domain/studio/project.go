package studio

import (
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"
)

type Kind string

const (
	KindImage Kind = "image"
	KindVideo Kind = "video"
)

const MaxNameRunes = 120

type Project struct {
	ID          string
	WorkspaceID string
	Kind        Kind
	Name        string
	Document    json.RawMessage
	Version     int64
	CreatedBy   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type Summary struct {
	ID        string
	Kind      Kind
	Name      string
	Version   int64
	UpdatedAt time.Time
}

func NewProject(workspaceID, createdBy string, kind Kind, name string, document json.RawMessage) (*Project, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, ErrWorkspaceRequired
	}
	if strings.TrimSpace(createdBy) == "" {
		return nil, ErrCreatorRequired
	}
	clean, err := cleanName(name)
	if err != nil {
		return nil, err
	}
	if err := ValidateDocument(kind, document); err != nil {
		return nil, err
	}
	return &Project{WorkspaceID: workspaceID, CreatedBy: createdBy, Kind: kind, Name: clean, Document: document, Version: 1}, nil
}

type Change struct {
	Name     *string
	Document json.RawMessage
}

func (p *Project) Apply(change Change) error {
	if change.Name == nil && change.Document == nil {
		return issue(FieldDocument, CodeRequired)
	}
	if change.Name != nil {
		clean, err := cleanName(*change.Name)
		if err != nil {
			return err
		}
		p.Name = clean
	}
	if change.Document != nil {
		if err := ValidateDocument(p.Kind, change.Document); err != nil {
			return err
		}
		p.Document = change.Document
	}
	return nil
}

func cleanName(name string) (string, error) {
	clean := strings.Join(strings.Fields(name), " ")
	if clean == "" {
		return "", issue(FieldName, CodeRequired)
	}
	if utf8.RuneCountInString(clean) > MaxNameRunes {
		return "", issue(FieldName, CodeTooLarge)
	}
	return clean, nil
}
