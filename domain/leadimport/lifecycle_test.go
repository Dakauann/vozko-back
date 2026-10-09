package leadimport

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"vozko/domain/lead"
	"vozko/domain/workspace"
)

func claimed(status Status) *Job {
	beat := now.Add(-time.Second)
	started := now.Add(-time.Minute)
	return &Job{ID: "imp-1", WorkspaceID: "ws-1", RequestedBy: "user-1", Status: status, Stage: StageRows, Claim: "c-1", Attempts: 1,
		HeartbeatAt: &beat, StartedAt: &started, Settings: &Settings{Policy: lead.PolicyFillEmpty}}
}

func TestAnalyzedRecordsTheDryRun(t *testing.T) {
	j := claimed(StatusAnalyzing)
	counts := Counts{Rows: 10, Created: 7}
	if err := j.Analyzed(counts, 10, now); err != nil {
		t.Fatal(err)
	}
	if j.Status != StatusAnalyzed || j.DryRun == nil || j.DryRun.Created != 7 || j.Processed != 10 || j.Claim != "" || !j.UpdatedAt.Equal(now) {
		t.Fatalf("job = %+v", j)
	}
	for _, status := range []Status{StatusUploaded, StatusImporting, StatusDone, StatusFailed} {
		if err := claimed(status).Analyzed(counts, 10, now); !errors.Is(err, ErrNotReady) {
			t.Errorf("Analyzed while %s = %v, want ErrNotReady", status, err)
		}
	}
}

func TestFinishClosesAnImport(t *testing.T) {
	j := claimed(StatusImporting)
	seed := &SeedOutcome{Queued: 3}
	if err := j.Finish(seed, now); err != nil {
		t.Fatal(err)
	}
	if j.Status != StatusDone || j.Seed != seed || j.FinishedAt == nil || !j.FinishedAt.Equal(now) || j.Claim != "" || !j.UpdatedAt.Equal(now) {
		t.Fatalf("job = %+v", j)
	}
	for _, status := range []Status{StatusAnalyzing, StatusAnalyzed, StatusDone, StatusFailed} {
		if err := claimed(status).Finish(nil, now); !errors.Is(err, ErrNotReady) {
			t.Errorf("Finish while %s = %v, want ErrNotReady", status, err)
		}
	}
}

func TestFailStopsAnImportForGood(t *testing.T) {
	for _, status := range []Status{StatusUploaded, StatusAnalyzing, StatusAnalyzed, StatusImporting} {
		j := claimed(status)
		if err := j.Fail(FailureForbidden, now); err != nil {
			t.Fatalf("Fail while %s: %v", status, err)
		}
		if j.Status != StatusFailed || j.FailureCode != FailureForbidden || j.FinishedAt == nil || j.Claim != "" || !j.UpdatedAt.Equal(now) {
			t.Fatalf("job = %+v", j)
		}
	}
	for _, status := range []Status{StatusDone, StatusFailed} {
		if err := claimed(status).Fail(FailureInternal, now); !errors.Is(err, ErrNotReady) {
			t.Errorf("Fail while %s = %v, want ErrNotReady", status, err)
		}
	}
}

func TestReleaseHandsTheImportBackToTheSweeper(t *testing.T) {
	j := claimed(StatusImporting)
	j.Processed = 1500
	if err := j.Release(now); err != nil {
		t.Fatal(err)
	}
	if j.Status != StatusImporting || j.Claim != "" || j.HeartbeatAt != nil || j.Processed != 1500 || j.Attempts != 1 || !j.UpdatedAt.Equal(now) {
		t.Fatalf("job = %+v", j)
	}
	if !j.Claimable(now) {
		t.Fatal("a released import is claimable again")
	}
	out := claimed(StatusImporting)
	out.Attempts = MaxAttempts
	if err := out.Release(now); err != nil || out.Claimable(now) {
		t.Fatalf("an import out of attempts is not claimed again, err %v", err)
	}
	for _, status := range []Status{StatusUploaded, StatusAnalyzed, StatusDone, StatusFailed} {
		if err := claimed(status).Release(now); !errors.Is(err, ErrNotReady) {
			t.Errorf("Release while %s = %v, want ErrNotReady", status, err)
		}
	}
}

func TestFailureOfTellsPermanentCausesFromPassingOnes(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		code      FailureCode
		permanent bool
	}{
		{"a missing permission", &PermissionError{Action: workspace.ActionAssign}, FailureForbidden, true},
		{"a file that is gone", fmt.Errorf("%w: gone", ErrFileUnavailable), FailureFileUnavailable, true},
		{"rows that only lived in a request", ErrInterrupted, FailureInterrupted, true},
		{"a job in the wrong state", ErrNotReady, FailureInternal, true},
		{"a mapping that no longer fits", &MappingError{Column: 1, Field: "x", Rule: RuleUnknownField}, FailureInternal, true},
		{"a file that is not a sheet", ErrUnsupportedFile, FailureInternal, true},
		{"a dropped connection", errors.New("connection reset by peer"), FailureInternal, false},
		{"a timeout", fmt.Errorf("write: %w", errors.New("deadline exceeded")), FailureInternal, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, permanent := FailureOf(tc.err)
			if code != tc.code || permanent != tc.permanent {
				t.Fatalf("FailureOf = %s, %v; want %s, %v", code, permanent, tc.code, tc.permanent)
			}
		})
	}
}

func TestUploadsToEraseKeepsRoomForANewUpload(t *testing.T) {
	jobs := make([]Job, 0, MaxUnusedUploads+1)
	for i := 0; i < MaxUnusedUploads+1; i++ {
		jobs = append(jobs, Job{ID: fmt.Sprintf("imp-%d", i), Status: StatusUploaded, CreatedAt: now.Add(time.Duration(i) * time.Minute)})
	}
	cases := []struct {
		name   string
		unused []Job
		want   []string
	}{
		{"below the cap", jobs[:MaxUnusedUploads-1], nil},
		{"at the cap the oldest goes", []Job{jobs[2], jobs[0], jobs[1], jobs[3], jobs[4]}, []string{"imp-0"}},
		{"over the cap the oldest ones go", jobs, []string{"imp-0", "imp-1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, j := range UploadsToErase(tc.unused) {
				got = append(got, j.ID)
			}
			if fmt.Sprint(got) != fmt.Sprint(tc.want) {
				t.Fatalf("erase %v, want %v", got, tc.want)
			}
		})
	}
	if !(Job{Status: StatusUploaded}).Unused() || !(Job{Status: StatusAnalyzed}).Unused() || !(Job{Status: StatusFailed}).Unused() {
		t.Fatal("uploaded, analyzed and failed before starting are unused")
	}
	started := now
	if (Job{Status: StatusFailed, StartedAt: &started}).Unused() || (Job{Status: StatusImporting}).Unused() || (Job{Status: StatusDone}).Unused() {
		t.Fatal("a started, running or finished import is not an unused upload")
	}
}
