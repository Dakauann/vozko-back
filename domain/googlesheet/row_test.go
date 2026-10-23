package googlesheet

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestIdempotencyKeys(t *testing.T) {
	a := KeyForTool("entry-1", "call-1", map[string]any{"nome": "Ana", "valor": 1})
	b := KeyForTool("entry-1", "call-1", map[string]any{"valor": 1, "nome": "Ana"})
	c := KeyForTool("entry-1", "call-1", map[string]any{"nome": "Bia"})
	if a != b {
		t.Fatalf("the same values in another order made another key: %s vs %s", a, b)
	}
	if a == c {
		t.Fatal("different values reused a key")
	}
	if !strings.HasPrefix(a, "ai:entry-1:call-1:") {
		t.Fatalf("tool key = %q", a)
	}
	if got := KeyForNode("run-1", "node-1#3"); got != "wf:run-1:node-1#3" {
		t.Fatalf("node key = %q", got)
	}
	if got := KeyForCopilot("action-1"); got != "elo:action-1" {
		t.Fatalf("copilot key = %q", got)
	}
	if got := KeyForManual("user-1", " abc "); got != "manual:user-1:abc" {
		t.Fatalf("manual key = %q", got)
	}
	for _, key := range []string{a, KeyForNode(strings.Repeat("r", 300), "n")} {
		if len(key) > MaxIdempotencyKeyLength {
			t.Fatalf("key %q is longer than %d", key, MaxIdempotencyKeyLength)
		}
	}
}

func TestNewRowRefusesMissingIdentity(t *testing.T) {
	s := typedSheet(t)
	s.ID = "sheet-1"
	if _, err := NewRow(s, Cells{"nome": "a"}, "", Origin{Kind: OriginAgent}, acceptNow); !errors.Is(err, ErrIdempotencyKeyRequired) {
		t.Fatalf("err = %v", err)
	}
	if _, err := NewRow(s, Cells{"nome": "a"}, "k", Origin{}, acceptNow); !errors.Is(err, ErrOriginRequired) {
		t.Fatalf("err = %v", err)
	}
	row, err := NewRow(s, Cells{"nome": "a"}, "k", Origin{Kind: OriginAgent, AgentID: "agent-1"}, acceptNow)
	if err != nil || row.Status != RowPending || row.SheetVersion != s.Version || !row.NextAttemptAt.Equal(acceptNow) {
		t.Fatalf("row = %+v, %v", row, err)
	}
}

func TestBackoffFollowsGoogleGuidance(t *testing.T) {
	for attempt, want := range map[int]time.Duration{1: 2 * time.Second, 2: 4 * time.Second, 5: 32 * time.Second, 9: MaxBackoff} {
		got := Backoff(attempt, 0)
		if got != want {
			t.Fatalf("Backoff(%d) = %v, want %v", attempt, got, want)
		}
	}
	if Backoff(1, 900*time.Millisecond) != 2900*time.Millisecond {
		t.Fatal("jitter not added")
	}
}

func TestPlanBatchRespectsRowsAndBytes(t *testing.T) {
	cases := []struct {
		name  string
		sizes []int
		rows  int
		bytes int
		want  int
	}{
		{name: "everything fits", sizes: []int{10, 10, 10}, rows: 500, bytes: 100, want: 3},
		{name: "row cap", sizes: []int{1, 1, 1, 1}, rows: 2, bytes: 100, want: 2},
		{name: "byte cap", sizes: []int{40, 40, 40}, rows: 500, bytes: 100, want: 2},
		{name: "first row alone over the cap still goes alone", sizes: []int{150, 10}, rows: 500, bytes: 100, want: 1},
		{name: "nothing", rows: 500, bytes: 100, want: 0},
	}
	for _, c := range cases {
		if got := PlanBatch(c.sizes, c.rows, c.bytes); got != c.want {
			t.Fatalf("%s: PlanBatch = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestDecideOutcome(t *testing.T) {
	cases := []struct {
		failure  Failure
		attempts int
		want     Outcome
	}{
		{failure: Failure{Kind: FailureRateLimited}, attempts: 1, want: OutcomeRetry},
		{failure: Failure{Kind: FailureTransient}, attempts: MaxTransientAttempts, want: OutcomeHold},
		{failure: Failure{Kind: FailureTransient}, attempts: 2, want: OutcomeRetry},
		{failure: Failure{Kind: FailureAuth}, attempts: 1, want: OutcomeWaitForAccount},
		{failure: Failure{Kind: FailurePermission}, attempts: 1, want: OutcomeHold},
		{failure: Failure{Kind: FailureNotFound}, attempts: 1, want: OutcomeHold},
		{failure: Failure{Kind: FailureInvalid}, attempts: 1, want: OutcomeIsolate},
	}
	for _, c := range cases {
		if got := Decide(c.failure, c.attempts); got != c.want {
			t.Fatalf("Decide(%s, %d) = %s, want %s", c.failure.Kind, c.attempts, got, c.want)
		}
	}
}

func TestEveryErrorHasAStableCode(t *testing.T) {
	seen := map[string]bool{}
	for _, err := range AllErrors() {
		code := ErrorCode(err)
		if code == CodeInternal || code == "" {
			t.Fatalf("%v has no code", err)
		}
		if seen[code] {
			t.Fatalf("code %s used twice", code)
		}
		seen[code] = true
	}
	wrapped := &ValueError{Column: "nome", Reason: ErrRequiredColumnMissing}
	if ErrorCode(wrapped) != CodeRequiredColumnMissing {
		t.Fatalf("wrapped code = %s", ErrorCode(wrapped))
	}
}

func TestEveryFailureKindHasARowCode(t *testing.T) {
	for _, kind := range []FailureKind{FailureAuth, FailurePermission, FailureNotFound, FailureRateLimited, FailureInvalid, FailureTransient} {
		code := Failure{Kind: kind}.Code()
		found := false
		for _, known := range RowErrorCodes() {
			found = found || known == code
		}
		if !found {
			t.Fatalf("%s maps to %q, which is not listed for translation", kind, code)
		}
	}
}
