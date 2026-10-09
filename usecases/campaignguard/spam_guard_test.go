package campaignguard

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	wsc "vozko/domain/workspace_config"
)

var guardNow = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

func clock() time.Time { return guardNow }

func TestNewSpamGuardRefusesAMissingDependency(t *testing.T) {
	if _, err := NewSpamGuard(nil, &sendLog{}, newClaimBook(), clock); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("no policy: err = %v", err)
	}
	if _, err := NewSpamGuard(&fixedDays{}, nil, newClaimBook(), clock); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("no send log: err = %v", err)
	}
	if _, err := NewSpamGuard(&fixedDays{}, &sendLog{}, newClaimBook(), nil); err != nil {
		t.Fatalf("a missing clock defaults to now: err = %v", err)
	}
}

func TestInCooldown(t *testing.T) {
	recent := guardNow.Add(-24 * time.Hour)
	old := guardNow.Add(-10 * 24 * time.Hour)
	cases := []struct {
		name    string
		days    *fixedDays
		sends   *sendLog
		lead    string
		sender  string
		want    bool
		wantErr bool
	}{
		{name: "sent yesterday with a 3 day window", days: &fixedDays{days: 3}, sends: &sendLog{last: map[string]time.Time{"l-1": recent}}, lead: "l-1", sender: "bp-1", want: true},
		{name: "sent ten days ago", days: &fixedDays{days: 3}, sends: &sendLog{last: map[string]time.Time{"l-1": old}}, lead: "l-1", sender: "bp-1"},
		{name: "never sent", days: &fixedDays{days: 3}, sends: &sendLog{}, lead: "l-1", sender: "bp-1"},
		{name: "the workspace turned the window off", days: &fixedDays{days: 0}, sends: &sendLog{last: map[string]time.Time{"l-1": recent}}, lead: "l-1", sender: "bp-1"},
		{name: "an unreadable policy refuses", days: &fixedDays{err: errBoom}, sends: &sendLog{}, lead: "l-1", sender: "bp-1", wantErr: true},
		{name: "an unreadable send log refuses", days: &fixedDays{days: 3}, sends: &sendLog{err: errBoom}, lead: "l-1", sender: "bp-1", wantErr: true},
		{name: "no sender means nothing was sent from it", days: &fixedDays{days: 3}, sends: &sendLog{last: map[string]time.Time{"l-1": recent}}, lead: "l-1", sender: " "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			guard, err := NewSpamGuard(tc.days, tc.sends, newClaimBook(), clock)
			if err != nil {
				t.Fatal(err)
			}
			got, err := guard.InCooldown(context.Background(), "ws-1", tc.lead, tc.sender)
			if (err != nil) != tc.wantErr || got != tc.want {
				t.Fatalf("InCooldown() = (%v, %v), want (%v, err %v)", got, err, tc.want, tc.wantErr)
			}
			if tc.wantErr && got {
				t.Fatal("an error must not also report a verdict")
			}
		})
	}
}

func TestInCooldownManyReadsInBoundedBatchesAndFailsClosed(t *testing.T) {
	recent := guardNow.Add(-time.Hour)
	ids := make([]string, 0, 12001)
	sends := &sendLog{last: map[string]time.Time{}}
	for i := 0; i < 12000; i++ {
		id := fmt.Sprintf("l-%05d", i)
		ids = append(ids, id)
		if i%1000 == 0 {
			sends.last[id] = recent
		}
	}
	ids = append(ids, "l-00000")
	days := &fixedDays{days: 3}
	guard, _ := NewSpamGuard(days, sends, newClaimBook(), clock)

	got, err := guard.InCooldownMany(context.Background(), "ws-1", ids, "bp-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 12 || !got["l-00000"] || !got["l-11000"] || got["l-00001"] {
		t.Fatalf("InCooldownMany() marked %d leads, want the 12 recent ones", len(got))
	}
	if len(sends.batches) != 3 {
		t.Fatalf("batches = %d, want 3 reads of at most %d", len(sends.batches), FactsChunk)
	}
	for _, batch := range sends.batches {
		if len(batch) > FactsChunk {
			t.Fatalf("a batch of %d ids exceeds the chunk", len(batch))
		}
	}
	if days.calls != 1 {
		t.Fatalf("the policy was read %d times, want once per call", days.calls)
	}

	failing, _ := NewSpamGuard(&fixedDays{days: 3}, &sendLog{err: errBoom}, newClaimBook(), clock)
	if out, err := failing.InCooldownMany(context.Background(), "ws-1", ids, "bp-1"); err == nil || out != nil {
		t.Fatalf("a failed read must refuse the whole batch, got (%v, %v)", len(out), err)
	}
	empty, err := guard.InCooldownMany(context.Background(), "ws-1", nil, "bp-1")
	if err != nil || len(empty) != 0 {
		t.Fatalf("no leads: (%v, %v)", empty, err)
	}
}

func TestRecordGoesToTheSendLog(t *testing.T) {
	sends := &sendLog{}
	guard, _ := NewSpamGuard(&fixedDays{}, sends, newClaimBook(), clock)
	if err := guard.Record("l-1", "bp-1", "c-1"); err != nil || len(sends.recorded) != 1 || sends.recorded[0] != "l-1|bp-1|c-1" {
		t.Fatalf("Record() = %v, recorded %v", err, sends.recorded)
	}
	sends.recordErr = errBoom
	if err := guard.Record("l-1", "bp-1", "c-1"); !errors.Is(err, errBoom) {
		t.Fatalf("Record() must surface the error, got %v", err)
	}
}

type configs struct {
	cfg *wsc.WorkspaceConfig
	err error
}

func (c configs) GetByWorkspaceID(context.Context, string) (*wsc.WorkspaceConfig, error) {
	return c.cfg, c.err
}

func configPolicy(t *testing.T, c WorkspaceConfigs) SpamPolicy {
	t.Helper()
	policy, err := NewConfigSpamPolicy(c)
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

func TestConfigSpamPolicyFailsClosed(t *testing.T) {
	days, err := configPolicy(t, configs{cfg: &wsc.WorkspaceConfig{CampaignSpamProtectionDays: 5}}).SpamProtectionDays(context.Background(), "ws-1")
	if err != nil || days != 5 {
		t.Fatalf("SpamProtectionDays() = (%d, %v), want 5", days, err)
	}
	if _, err := configPolicy(t, configs{err: errBoom}).SpamProtectionDays(context.Background(), "ws-1"); !errors.Is(err, errBoom) {
		t.Fatalf("an unreadable config must refuse, got %v", err)
	}
	if _, err := configPolicy(t, configs{}).SpamProtectionDays(context.Background(), "ws-1"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("a missing config must refuse, got %v", err)
	}
	if policy, err := NewConfigSpamPolicy(nil); !errors.Is(err, ErrUnavailable) || policy != nil {
		t.Fatalf("no config reader must stop the build, got (%v, %v)", policy, err)
	}
}
