package studio

import (
	"errors"
	"strings"
	"testing"
)

func report() CapabilityReport {
	return CapabilityReport{
		SessionID:   "6f1c2a8e-3b4d-4e5f-8a9b-0c1d2e3f4a5b",
		WorkspaceID: "ws-1",
		UserID:      "u-1",
		Kind:        KindVideo,
		UserAgent:   "Mozilla/5.0",
		Capabilities: Capabilities{
			Backend: BackendWebGL, GPUVendor: " Google Inc. (Intel) ", GPURenderer: "ANGLE (Intel, Iris Xe)",
			WebGPU: true, Decode: true, EncodeVideo: true, EncodeAudio: true, PixelRatio: 1.5, Cores: 8, MemoryGB: 8,
		},
		Usage: Usage{Frames: 1200, SlowFrames: 4, Stalls: 1, BrowserExports: 1, ExportFailures: map[ExportFailure]int64{FailureNoEncoder: 1}},
	}
}

func codes(t *testing.T, err error) map[string]string {
	t.Helper()
	var invalid *ValidationError
	if !errors.As(err, &invalid) {
		t.Fatalf("want a validation error, got %v", err)
	}
	return invalid.Codes()
}

func TestACapabilityReportIsKeptTidy(t *testing.T) {
	r := report()
	r.UserAgent = strings.Repeat("a", MaxUserAgentRunes+50)
	got, err := NewCapabilityReport(r)
	if err != nil {
		t.Fatal(err)
	}
	if got.Capabilities.GPUVendor != "Google Inc. (Intel)" || len([]rune(got.UserAgent)) != MaxUserAgentRunes {
		t.Fatalf("report = %+v", got)
	}
}

func TestACapabilityReportNeedsItsOwnerAndSession(t *testing.T) {
	missingWorkspace := report()
	missingWorkspace.WorkspaceID = ""
	if _, err := NewCapabilityReport(missingWorkspace); !errors.Is(err, ErrWorkspaceRequired) {
		t.Fatalf("workspace: %v", err)
	}
	missingUser := report()
	missingUser.UserID = ""
	if _, err := NewCapabilityReport(missingUser); !errors.Is(err, ErrUserRequired) {
		t.Fatalf("user: %v", err)
	}
	badSession := report()
	badSession.SessionID = "not-a-session"
	if got := codes(t, func() error { _, err := NewCapabilityReport(badSession); return err }()); got[FieldSession] != CodeInvalid {
		t.Fatalf("session codes = %v", got)
	}
}

func TestACapabilityReportRefusesValuesOutsideWhatBrowsersSend(t *testing.T) {
	r := report()
	r.Kind = "audio"
	r.Capabilities.Backend = "canvas"
	r.Capabilities.PixelRatio = 40
	r.Usage.Frames = -1
	r.Usage.ExportFailures = map[ExportFailure]int64{"bored": 1}
	_, err := NewCapabilityReport(r)
	got := codes(t, err)
	want := map[string]string{FieldKind: CodeUnknown, FieldBackend: CodeUnknown, FieldDevice: CodeOutOfRange, FieldUsage: CodeOutOfRange, FieldFailures: CodeUnknown}
	for field, code := range want {
		if got[field] != code {
			t.Fatalf("%s = %q, codes %v", field, got[field], got)
		}
	}
}
