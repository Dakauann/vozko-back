package imagegen

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
)

const (
	Exchange     = "image_generation_exchange"
	Topic        = "image.generate"
	ActiveWindow = 10 * time.Minute
)

type QueueMessage struct {
	JobID string `json:"jobId"`
}

type Status string

const (
	StatusQueued  Status = "queued"
	StatusRunning Status = "running"
	StatusDone    Status = "done"
	StatusFailed  Status = "failed"
)

func (s Status) Terminal() bool { return s == StatusDone || s == StatusFailed }

func (s Status) Known() bool {
	switch s {
	case StatusQueued, StatusRunning, StatusDone, StatusFailed:
		return true
	}
	return false
}

type FailureCode string

const (
	FailureGeneration           FailureCode = "generation_failed"
	FailureStorage              FailureCode = "storage_failed"
	FailureTimedOut             FailureCode = "timed_out"
	FailureEnqueue              FailureCode = "enqueue_failed"
	FailureInsufficientFunds    FailureCode = "insufficient_funds"
	FailureReferenceUnavailable FailureCode = "reference_unavailable"
)

func (c FailureCode) Known() bool {
	switch c {
	case FailureGeneration, FailureStorage, FailureTimedOut, FailureEnqueue, FailureInsufficientFunds, FailureReferenceUnavailable:
		return true
	}
	return false
}

type Job struct {
	ID                string
	WorkspaceID       string
	RequestedBy       string
	Prompt            string
	Aspect            Aspect
	ReferenceMediaIDs []string
	Fingerprint       string
	Status            Status
	MediaID           string
	MediaURL          string
	Model             string
	FailureCode       FailureCode
	Attempts          int
	CreatedAt         time.Time
	UpdatedAt         time.Time
	FinishedAt        *time.Time
}

type Result struct {
	MediaID  string
	MediaURL string
	Model    string
}

func NewJob(req Request, requestedBy string) (*Job, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	requester := strings.TrimSpace(requestedBy)
	if requester == "" {
		return nil, ErrRequesterRequired
	}
	model := strings.TrimSpace(req.Model)
	prompt := strings.TrimSpace(req.Prompt)
	references := trimmedReferences(req.ReferenceMediaIDs)
	return &Job{
		WorkspaceID:       req.WorkspaceID,
		RequestedBy:       requester,
		Model:             model,
		Prompt:            prompt,
		Aspect:            req.Aspect,
		ReferenceMediaIDs: references,
		Fingerprint:       Fingerprint(req.WorkspaceID, requester, model, prompt, req.Aspect, references...),
		Status:            StatusQueued,
	}, nil
}

func (j *Job) Request() Request {
	return Request{WorkspaceID: j.WorkspaceID, Model: j.Model, Prompt: j.Prompt, Aspect: j.Aspect, ReferenceMediaIDs: j.ReferenceMediaIDs}
}

func Fingerprint(workspaceID, requestedBy, model, prompt string, aspect Aspect, references ...string) string {
	parts := []string{workspaceID, requestedBy, model, normalizePrompt(prompt), string(aspect)}
	if len(references) > 0 {
		parts = append(parts, strconv.Itoa(len(references)))
		for _, id := range references {
			parts = append(parts, strconv.Itoa(len(id)), id)
		}
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

func normalizePrompt(prompt string) string {
	return strings.Join(strings.Fields(prompt), " ")
}

func ActiveSince(now time.Time) time.Time { return now.Add(-ActiveWindow) }
