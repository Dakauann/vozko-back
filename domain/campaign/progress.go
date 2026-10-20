package campaign

import (
	"math"
	"time"
)

const (
	runningProgressRefresh   = 3 * time.Second
	runningProgressFreshness = 2 * time.Second
	idleProgressFreshness    = 30 * time.Second
	paceMinimumWindow        = 20 * time.Second
	paceBaselineMaxAge       = 2 * time.Minute
)

type Progress struct {
	Status              Status    `json:"status"`
	Metrics             *Metrics  `json:"metrics"`
	Pace                *Pace     `json:"pace,omitempty"`
	SampledAt           time.Time `json:"sampledAt"`
	RefreshAfterSeconds int       `json:"refreshAfterSeconds"`
}

type Pace struct {
	ProcessedPerMinute float64 `json:"processedPerMinute"`
	RemainingSeconds   *int64  `json:"remainingSeconds,omitempty"`
}

type ProgressSample struct {
	Processed int64     `json:"processed"`
	At        time.Time `json:"at"`
}

func ProgressRefresh(status Status) time.Duration {
	if status == StatusRunning {
		return runningProgressRefresh
	}
	return 0
}

func ProgressFreshness(status Status) time.Duration {
	if status == StatusRunning {
		return runningProgressFreshness
	}
	return idleProgressFreshness
}

func NewProgress(status Status, counts *Counts, at time.Time, baseline *ProgressSample) Progress {
	metrics := NewMetrics(counts)
	return Progress{
		Status:              status,
		Metrics:             metrics,
		Pace:                paceSince(status, metrics, at, baseline),
		SampledAt:           at,
		RefreshAfterSeconds: int(ProgressRefresh(status) / time.Second),
	}
}

func NextProgressBaseline(baseline *ProgressSample, current ProgressSample) ProgressSample {
	if baseline == nil || baseline.Processed > current.Processed || current.At.Sub(baseline.At) > paceBaselineMaxAge {
		return current
	}
	return *baseline
}

func paceSince(status Status, metrics *Metrics, at time.Time, baseline *ProgressSample) *Pace {
	if status != StatusRunning || baseline == nil || baseline.Processed > metrics.Processed {
		return nil
	}
	window := at.Sub(baseline.At)
	if window < paceMinimumWindow {
		return nil
	}
	perMinute := float64(metrics.Processed-baseline.Processed) / window.Minutes()
	pace := &Pace{ProcessedPerMinute: math.Round(perMinute*10) / 10}
	if perMinute > 0 && metrics.Pending > 0 {
		remaining := int64(math.Ceil(float64(metrics.Pending) / perMinute * 60))
		pace.RemainingSeconds = &remaining
	}
	return pace
}
