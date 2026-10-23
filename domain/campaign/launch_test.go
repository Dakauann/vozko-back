package campaign

import (
	"errors"
	"testing"
	"time"
)

func TestParseLaunchAcceptsTheThreeModes(t *testing.T) {
	at := time.Date(2026, 10, 23, 9, 0, 0, 0, time.FixedZone("BRT", -3*3600))
	cases := []struct {
		mode string
		at   *time.Time
		want Launch
	}{
		{"now", nil, Launch{Mode: LaunchNow}},
		{" NOW ", nil, Launch{Mode: LaunchNow}},
		{"ready", nil, Launch{Mode: LaunchReady}},
		{"schedule", &at, Launch{Mode: LaunchSchedule, At: at.UTC()}},
	}
	for _, c := range cases {
		got, err := ParseLaunch(c.mode, c.at)
		if err != nil {
			t.Fatalf("%q: %v", c.mode, err)
		}
		if got != c.want {
			t.Fatalf("%q: got %+v, want %+v", c.mode, got, c.want)
		}
	}
}

func TestParseLaunchRefusesAnUnknownMode(t *testing.T) {
	if _, err := ParseLaunch("later", nil); !errors.Is(err, ErrLaunchModeInvalid) {
		t.Fatalf("got %v", err)
	}
	if _, err := ParseLaunch("", nil); !errors.Is(err, ErrLaunchModeInvalid) {
		t.Fatalf("empty: got %v", err)
	}
}

func TestParseLaunchNeedsATimeToSchedule(t *testing.T) {
	if _, err := ParseLaunch("schedule", nil); !errors.Is(err, ErrLaunchScheduleMissing) {
		t.Fatalf("got %v", err)
	}
	zero := time.Time{}
	if _, err := ParseLaunch("schedule", &zero); !errors.Is(err, ErrLaunchScheduleMissing) {
		t.Fatalf("zero: got %v", err)
	}
}

func TestParseLaunchIgnoresATimeOutsideSchedule(t *testing.T) {
	at := time.Now()
	for _, mode := range []string{"now", "ready"} {
		got, err := ParseLaunch(mode, &at)
		if err != nil {
			t.Fatal(err)
		}
		if !got.At.IsZero() {
			t.Fatalf("%s kept the time %v", mode, got.At)
		}
	}
}

func TestLaunchCodes(t *testing.T) {
	if ErrorCode(ErrLaunchModeInvalid) != "campaign_launch_mode_invalid" {
		t.Fatal(ErrorCode(ErrLaunchModeInvalid))
	}
	if ErrorCode(ErrLaunchScheduleMissing) != "campaign_launch_schedule_missing" {
		t.Fatal(ErrorCode(ErrLaunchScheduleMissing))
	}
}

func TestReadyToStartIsANeverSentStoppedCampaign(t *testing.T) {
	fresh := &Metrics{TotalNumbers: 10, Pending: 8, Failed: 2}
	if !fresh.ReadyToStart(StatusStopped, false) {
		t.Fatal("a stopped campaign with pending contacts and nothing sent is ready")
	}
	if fresh.ReadyToStart(StatusStopped, true) {
		t.Fatal("a scheduled campaign is not shown as ready")
	}
	for _, status := range []Status{StatusRunning, StatusPaused, StatusCompleted} {
		if fresh.ReadyToStart(status, false) {
			t.Fatalf("%s is not ready", status)
		}
	}
	sent := &Metrics{TotalNumbers: 10, Pending: 9, Sent: 1}
	if sent.ReadyToStart(StatusStopped, false) {
		t.Fatal("a campaign that already sent is stopped, not ready")
	}
	empty := &Metrics{TotalNumbers: 3, Failed: 3}
	if empty.ReadyToStart(StatusStopped, false) {
		t.Fatal("nothing pending is not ready")
	}
	var missing *Metrics
	if missing.ReadyToStart(StatusStopped, false) {
		t.Fatal("unknown metrics are not ready")
	}
}
