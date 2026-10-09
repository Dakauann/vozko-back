package studio

import (
	"strings"

	"github.com/google/uuid"
)

type RenderBackend string

const (
	BackendWebGL       RenderBackend = "webgl"
	BackendWebGPU      RenderBackend = "webgpu"
	BackendUnavailable RenderBackend = "unavailable"
	BackendDisabled    RenderBackend = "disabled"
)

var renderBackends = map[RenderBackend]bool{BackendWebGL: true, BackendWebGPU: true, BackendUnavailable: true, BackendDisabled: true}

type ExportFailure string

const (
	FailureNoEncoder   ExportFailure = "no_encoder"
	FailureUndecodable ExportFailure = "undecodable"
	FailureStalled     ExportFailure = "stalled"
	FailureTooLarge    ExportFailure = "too_large"
	FailureRendererOff ExportFailure = "renderer_off"
	FailureUpload      ExportFailure = "upload_failed"
	FailureFailed      ExportFailure = "failed"
)

var exportFailures = map[ExportFailure]bool{
	FailureNoEncoder: true, FailureUndecodable: true, FailureStalled: true, FailureTooLarge: true, FailureRendererOff: true, FailureUpload: true, FailureFailed: true,
}

const (
	MaxCapabilityTextRunes = 200
	MaxUserAgentRunes      = 400
	MaxUsageCount          = 1_000_000_000
	MaxPixelRatio          = 16
	MaxCores               = 1024
	MaxMemoryGB            = 1024
)

const (
	FieldSession  = "sessionId"
	FieldBackend  = "capabilities.backend"
	FieldDevice   = "capabilities.device"
	FieldUsage    = "usage"
	FieldFailures = "usage.exportFailures"
)

type Capabilities struct {
	Backend     RenderBackend
	GPUVendor   string
	GPURenderer string
	WebGPU      bool
	Decode      bool
	EncodeVideo bool
	EncodeAudio bool
	PixelRatio  float64
	Cores       int
	MemoryGB    float64
}

type Usage struct {
	Frames          int64
	SlowFrames      int64
	Stalls          int64
	ContextLosses   int64
	DecodeFallbacks int64
	WorkerFailures  int64
	BrowserExports  int64
	ExportFailures  map[ExportFailure]int64
}

type CapabilityReport struct {
	SessionID    string
	WorkspaceID  string
	UserID       string
	Kind         Kind
	UserAgent    string
	Capabilities Capabilities
	Usage        Usage
}

func NewCapabilityReport(r CapabilityReport) (CapabilityReport, error) {
	if strings.TrimSpace(r.WorkspaceID) == "" {
		return CapabilityReport{}, ErrWorkspaceRequired
	}
	if strings.TrimSpace(r.UserID) == "" {
		return CapabilityReport{}, ErrUserRequired
	}
	if issues := r.issues(); len(issues) > 0 {
		return CapabilityReport{}, &ValidationError{Issues: issues}
	}
	r.UserAgent = clipped(r.UserAgent, MaxUserAgentRunes)
	r.Capabilities.GPUVendor = clipped(r.Capabilities.GPUVendor, MaxCapabilityTextRunes)
	r.Capabilities.GPURenderer = clipped(r.Capabilities.GPURenderer, MaxCapabilityTextRunes)
	return r, nil
}

func (r CapabilityReport) issues() []FieldIssue {
	var issues []FieldIssue
	add := func(field, code string) { issues = append(issues, FieldIssue{Field: field, Code: code}) }
	if _, err := uuid.Parse(r.SessionID); err != nil {
		add(FieldSession, CodeInvalid)
	}
	if r.Kind != KindImage && r.Kind != KindVideo {
		add(FieldKind, CodeUnknown)
	}
	if !renderBackends[r.Capabilities.Backend] {
		add(FieldBackend, CodeUnknown)
	}
	if !r.Capabilities.deviceInRange() {
		add(FieldDevice, CodeOutOfRange)
	}
	if !r.Usage.countsInRange() {
		add(FieldUsage, CodeOutOfRange)
	}
	if code := r.Usage.failureIssue(); code != "" {
		add(FieldFailures, code)
	}
	return issues
}

func (c Capabilities) deviceInRange() bool {
	return within(c.PixelRatio, 0, MaxPixelRatio) && c.Cores >= 0 && c.Cores <= MaxCores && within(c.MemoryGB, 0, MaxMemoryGB)
}

func (u Usage) countsInRange() bool {
	for _, count := range []int64{u.Frames, u.SlowFrames, u.Stalls, u.ContextLosses, u.DecodeFallbacks, u.WorkerFailures, u.BrowserExports} {
		if !countable(count) {
			return false
		}
	}
	return true
}

func (u Usage) failureIssue() string {
	for reason, count := range u.ExportFailures {
		if !exportFailures[reason] {
			return CodeUnknown
		}
		if !countable(count) {
			return CodeOutOfRange
		}
	}
	return ""
}

func countable(count int64) bool {
	return count >= 0 && count <= MaxUsageCount
}

func clipped(text string, max int) string {
	trimmed := []rune(strings.TrimSpace(text))
	if len(trimmed) > max {
		trimmed = trimmed[:max]
	}
	return string(trimmed)
}
