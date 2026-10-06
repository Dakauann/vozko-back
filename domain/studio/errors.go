package studio

import (
	"errors"
	"sort"
	"strings"
)

var (
	ErrInvalidDocument   = errors.New("studio: invalid project")
	ErrProjectNotFound   = errors.New("studio: project not found")
	ErrVersionConflict   = errors.New("studio: the project was saved elsewhere")
	ErrWorkspaceRequired = errors.New("studio: workspace id is required")
	ErrCreatorRequired   = errors.New("studio: the creator is required")
	ErrNotRasterized     = errors.New("studio: an overlay was not rasterized before export")
	ErrNotVideo          = errors.New("studio: only video projects are exported as video")
)

const (
	FieldKind     = "kind"
	FieldName     = "name"
	FieldDocument = "document"
	FieldCanvas   = "document.canvas"
	FieldLayers   = "document.layers"
	FieldTracks   = "document.tracks"
	FieldMarkers  = "document.markers"

	CodeRequired   = "required"
	CodeInvalid    = "invalid"
	CodeUnknown    = "unknown"
	CodeTooLarge   = "too_large"
	CodeTooMany    = "too_many"
	CodeOutOfRange = "out_of_range"
	CodeDuplicate  = "duplicate"
	CodeOverlap    = "overlap"
)

type FieldIssue struct {
	Field string
	Code  string
}

type ValidationError struct {
	Issues []FieldIssue
}

func (e *ValidationError) Error() string {
	parts := make([]string, 0, len(e.Issues))
	for _, i := range e.Issues {
		parts = append(parts, i.Field+" "+i.Code)
	}
	sort.Strings(parts)
	return ErrInvalidDocument.Error() + ": " + strings.Join(parts, ", ")
}

func (e *ValidationError) Unwrap() error { return ErrInvalidDocument }

func (e *ValidationError) Codes() map[string]string {
	codes := make(map[string]string, len(e.Issues))
	for _, i := range e.Issues {
		codes[i.Field] = i.Code
	}
	return codes
}

func issue(field, code string) error {
	return &ValidationError{Issues: []FieldIssue{{Field: field, Code: code}}}
}
