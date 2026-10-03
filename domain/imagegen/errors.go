package imagegen

import (
	"errors"
	"sort"
	"strings"
)

var (
	ErrWorkspaceRequired  = errors.New("imagegen: workspace id is required")
	ErrRequesterRequired  = errors.New("imagegen: the requesting user is required")
	ErrInvalidRequest     = errors.New("imagegen: invalid image request")
	ErrGenerationFailed   = errors.New("imagegen: image generation failed")
	ErrJobNotFound        = errors.New("imagegen: image generation job not found")
	ErrJobNotActive       = errors.New("imagegen: image generation job is no longer active")
	ErrDuplicateActiveJob = errors.New("imagegen: the same image is already being generated")
	ErrUnknownJobStatus   = errors.New("imagegen: image generation job has an unknown status")
	ErrUnknownFailureCode = errors.New("imagegen: unknown failure code")
)

const (
	FieldPrompt     = "prompt"
	FieldAspect     = "aspect"
	FieldReferences = "referenceMediaIds"

	CodeRequired  = "required"
	CodeTooLong   = "too_long"
	CodeUnknown   = "unknown"
	CodeTooMany   = "too_many"
	CodeDuplicate = "duplicate"
	CodeNotFound  = "not_found"
	CodeNotImage  = "not_image"
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
	for _, issue := range e.Issues {
		parts = append(parts, issue.Field+" "+issue.Code)
	}
	sort.Strings(parts)
	return ErrInvalidRequest.Error() + ": " + strings.Join(parts, ", ")
}

func (e *ValidationError) Unwrap() error { return ErrInvalidRequest }

func (e *ValidationError) Codes() map[string]string {
	codes := make(map[string]string, len(e.Issues))
	for _, issue := range e.Issues {
		codes[issue.Field] = issue.Code
	}
	return codes
}
