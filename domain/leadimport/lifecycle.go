package leadimport

import (
	"errors"
	"slices"
	"strings"
	"time"
)

const (
	MaxUnusedUploads = 5
	MaxListed        = 20
)

var ErrInterrupted = errors.New("lead import: the rows of this import only lived in the request that sent them")

func (j *Job) Analyzed(counts Counts, processed int, now time.Time) error {
	if j.Status != StatusAnalyzing {
		return ErrNotReady
	}
	dryRun := counts
	j.Status, j.DryRun, j.Processed, j.Claim, j.UpdatedAt = StatusAnalyzed, &dryRun, processed, "", now.UTC()
	return nil
}

func (j *Job) Finish(seed *SeedOutcome, now time.Time) error {
	if j.Status != StatusImporting {
		return ErrNotReady
	}
	at := now.UTC()
	j.Status, j.Seed, j.FinishedAt, j.Claim, j.UpdatedAt = StatusDone, seed, &at, "", at
	return nil
}

func (j *Job) Fail(code FailureCode, now time.Time) error {
	if j.Status == StatusDone || j.Status == StatusFailed {
		return ErrNotReady
	}
	at := now.UTC()
	j.Status, j.FailureCode, j.FinishedAt, j.Claim, j.UpdatedAt = StatusFailed, code, &at, "", at
	return nil
}

func (j *Job) Release(now time.Time) error {
	if !j.Status.Active() {
		return ErrNotReady
	}
	j.Claim, j.HeartbeatAt, j.UpdatedAt = "", nil, now.UTC()
	return nil
}

func (j Job) Unused() bool {
	switch j.Status {
	case StatusUploaded, StatusAnalyzed:
		return true
	case StatusFailed:
		return j.StartedAt == nil
	}
	return false
}

func UploadsToErase(unused []Job) []Job {
	if len(unused) < MaxUnusedUploads {
		return nil
	}
	oldest := slices.Clone(unused)
	slices.SortStableFunc(oldest, func(a, b Job) int { return a.CreatedAt.Compare(b.CreatedAt) })
	return oldest[:len(oldest)-MaxUnusedUploads+1]
}

func FailureOf(err error) (FailureCode, bool) {
	switch {
	case errors.Is(err, ErrForbidden):
		return FailureForbidden, true
	case errors.Is(err, ErrFileUnavailable):
		return FailureFileUnavailable, true
	case errors.Is(err, ErrInterrupted):
		return FailureInterrupted, true
	}
	for _, permanent := range []error{ErrNotReady, ErrMappingInvalid, ErrUnsupportedFile, ErrFileEmpty, ErrFileTooLarge, ErrTooManyRows} {
		if errors.Is(err, permanent) {
			return FailureInternal, true
		}
	}
	return FailureInternal, false
}

func Listed(jobs []Job, requestedBy string, now time.Time) []Job {
	requestedBy = strings.TrimSpace(requestedBy)
	if requestedBy == "" {
		return nil
	}
	out := make([]Job, 0, min(len(jobs), MaxListed))
	for _, j := range jobs {
		if j.VisibleTo(requestedBy) && !j.Expired(now) {
			out = append(out, j)
		}
	}
	slices.SortStableFunc(out, func(a, b Job) int { return b.CreatedAt.Compare(a.CreatedAt) })
	return out[:min(len(out), MaxListed)]
}
