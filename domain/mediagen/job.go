package mediagen

import (
	"fmt"
	"strings"
	"time"
)

const (
	Exchange     = "media_generation_exchange"
	Topic        = "media.generate"
	RenderTopic  = "media.render"
	ActiveWindow = 10 * time.Minute
)

type QueueMessage struct {
	JobID string `json:"jobId"`
}

type Status string

const (
	StatusQueued   Status = "queued"
	StatusRunning  Status = "running"
	StatusDone     Status = "done"
	StatusFailed   Status = "failed"
	StatusSettling Status = "settling"
)

const SettleWindow = 7 * 24 * time.Hour

const MaxActiveProcessing = 2

func (s Status) Terminal() bool { return s == StatusDone || s == StatusFailed }

func (s Status) Known() bool {
	switch s {
	case StatusQueued, StatusRunning, StatusDone, StatusFailed, StatusSettling:
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
	FailureCostUnreported       FailureCode = "cost_unreported"
)

func (c FailureCode) Known() bool {
	switch c {
	case FailureGeneration, FailureStorage, FailureTimedOut, FailureEnqueue, FailureInsufficientFunds, FailureReferenceUnavailable, FailureCostUnreported:
		return true
	}
	return false
}

type Job struct {
	ID                string
	WorkspaceID       string
	RequestedBy       string
	Kind              Kind
	Prompt            string
	Aspect            Aspect
	ReferenceMediaIDs []string
	Voice             string
	Video             Timeline
	SourceMediaID     string
	Fingerprint       string
	Status            Status
	MediaID           string
	MediaURL          string
	Model             string
	GenerationID      string
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
	n := req.normalized()
	return &Job{
		WorkspaceID:       n.WorkspaceID,
		RequestedBy:       requester,
		Kind:              n.Kind,
		Model:             n.Model,
		Prompt:            n.Prompt,
		Aspect:            n.Aspect,
		ReferenceMediaIDs: n.ReferenceMediaIDs,
		Voice:             n.Voice,
		Video:             n.Video,
		SourceMediaID:     n.SourceMediaID,
		Fingerprint:       n.Fingerprint(requester),
		Status:            StatusQueued,
	}, nil
}

func (j *Job) Request() Request {
	return Request{
		WorkspaceID: j.WorkspaceID, Kind: j.Kind, Model: j.Model, Prompt: j.Prompt, Aspect: j.Aspect,
		ReferenceMediaIDs: j.ReferenceMediaIDs, Voice: j.Voice, Video: j.Video, SourceMediaID: j.SourceMediaID,
	}
}

func ActiveSince(now time.Time) time.Time { return now.Add(-ActiveWindow) }

func SettleSince(now time.Time) time.Time { return now.Add(-SettleWindow) }

func (j *Job) Delivered() (Result, bool) {
	if j.Status != StatusDone || j.MediaID == "" {
		return Result{}, false
	}
	return Result{MediaID: j.MediaID, MediaURL: j.MediaURL, Model: j.Model}, true
}

type Settlement struct {
	GenerationID string
	Result       *Result
	Failure      FailureCode
}

func (s Settlement) Validate() error {
	if s.GenerationID == "" {
		return ErrCostUnreported
	}
	if (s.Result == nil) == (s.Failure == "") {
		return fmt.Errorf("%w: a settlement ends in either a result or a failure", ErrInvalidRequest)
	}
	if s.Failure != "" && !s.Failure.Known() {
		return fmt.Errorf("%w: %q", ErrUnknownFailureCode, s.Failure)
	}
	return nil
}

type ChargedFailure struct {
	GenerationID string
	Err          error
}

func (e *ChargedFailure) Error() string {
	return "mediagen: generation " + e.GenerationID + " failed after the provider started it: " + e.Err.Error()
}

func (e *ChargedFailure) Unwrap() error { return e.Err }

func Charged(generationID string, err error) error {
	if err == nil || generationID == "" {
		return err
	}
	return &ChargedFailure{GenerationID: generationID, Err: err}
}
