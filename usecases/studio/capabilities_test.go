package studio_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/studio"
)

type fakeReports struct {
	saved []studio.CapabilityReport
	err   error
}

func (f *fakeReports) Upsert(_ context.Context, report studio.CapabilityReport) error {
	f.saved = append(f.saved, report)
	return f.err
}

func capabilityReport() studio.CapabilityReport {
	return studio.CapabilityReport{
		SessionID: "6f1c2a8e-3b4d-4e5f-8a9b-0c1d2e3f4a5b", WorkspaceID: "ws-1", UserID: "u-1", Kind: studio.KindImage,
		Capabilities: studio.Capabilities{Backend: studio.BackendWebGL, GPURenderer: "  ANGLE  "},
	}
}

func TestCapabilitiesAreStoredOnlyWhenTheReportIsValid(t *testing.T) {
	reports := &fakeReports{}
	service, err := NewCapabilityService(reports)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Report(context.Background(), capabilityReport()); err != nil {
		t.Fatal(err)
	}
	if len(reports.saved) != 1 || reports.saved[0].Capabilities.GPURenderer != "ANGLE" {
		t.Fatalf("saved = %+v", reports.saved)
	}
	invalid := capabilityReport()
	invalid.Capabilities.Backend = "canvas"
	var validation *studio.ValidationError
	if err := service.Report(context.Background(), invalid); !errors.As(err, &validation) || len(reports.saved) != 1 {
		t.Fatalf("invalid report: err %v saved %d", err, len(reports.saved))
	}
}

func TestACapabilitySessionOfSomeoneElseIsRefused(t *testing.T) {
	service, _ := NewCapabilityService(&fakeReports{err: studio.ErrSessionTaken})
	if err := service.Report(context.Background(), capabilityReport()); !errors.Is(err, studio.ErrSessionTaken) {
		t.Fatalf("err = %v", err)
	}
	if _, err := NewCapabilityService(nil); !errors.Is(err, ErrMissingDependency) {
		t.Fatalf("missing repository: %v", err)
	}
}
