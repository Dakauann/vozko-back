package mediagen

import (
	"errors"
	"sort"
	"strings"
)

var (
	ErrWorkspaceRequired  = errors.New("mediagen: workspace id is required")
	ErrRequesterRequired  = errors.New("mediagen: the requesting user is required")
	ErrInvalidRequest     = errors.New("mediagen: invalid generation request")
	ErrGenerationFailed   = errors.New("mediagen: generation failed")
	ErrJobNotFound        = errors.New("mediagen: generation job not found")
	ErrJobNotActive       = errors.New("mediagen: generation job is no longer active")
	ErrDuplicateActiveJob = errors.New("mediagen: the same media is already being generated")
	ErrUnknownJobStatus   = errors.New("mediagen: generation job has an unknown status")
	ErrUnknownFailureCode = errors.New("mediagen: unknown failure code")
	ErrUnknownKind        = errors.New("mediagen: generation job has an unknown kind")
	ErrCostUnreported     = errors.New("mediagen: the provider did not report the cost of the generation")
	ErrTooManyActive      = errors.New("mediagen: the workspace already has the maximum of jobs of this kind running")
	ErrNoModels           = errors.New("mediagen: no model is available for this kind")
	ErrModelsUnavailable  = errors.New("mediagen: the model list is unavailable")
)

const (
	FieldPrompt     = "prompt"
	FieldAspect     = "aspect"
	FieldReferences = "referenceMediaIds"
	FieldModel      = "model"
	FieldKind       = "kind"
	FieldVoice      = "voice"
	FieldScenes     = "video.scenes"
	FieldTimeline   = "video.timeline"
	FieldSource     = "sourceMediaId"

	CodeRequired   = "required"
	CodeTooLong    = "too_long"
	CodeUnknown    = "unknown"
	CodeTooMany    = "too_many"
	CodeDuplicate  = "duplicate"
	CodeNotFound   = "not_found"
	CodeNotImage   = "not_image"
	CodeWrongType  = "wrong_type"
	CodeOutOfRange = "out_of_range"
	CodeUnexpected = "unexpected"
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
