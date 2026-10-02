package imagegen

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

var jobClock = time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

func TestOnlyDoneAndFailedAreTerminal(t *testing.T) {
	cases := map[Status]bool{StatusQueued: false, StatusRunning: false, StatusDone: true, StatusFailed: true}
	for status, terminal := range cases {
		if status.Terminal() != terminal || !status.Known() {
			t.Fatalf("%s terminal=%v known=%v", status, status.Terminal(), status.Known())
		}
	}
	if Status("paused").Known() {
		t.Fatal("unknown status accepted")
	}
}

func TestFailureCodesAreAClosedSet(t *testing.T) {
	for _, code := range []FailureCode{FailureGeneration, FailureStorage, FailureTimedOut, FailureEnqueue, FailureInsufficientFunds, FailureReferenceUnavailable} {
		if !code.Known() {
			t.Fatalf("%s rejected", code)
		}
	}
	if FailureCode("oops").Known() {
		t.Fatal("unknown failure code accepted")
	}
}

func TestFingerprintIgnoresSpacingButNotTheRequester(t *testing.T) {
	a := Fingerprint("ws", "u1", "  pizza   artesanal\n", AspectSquare)
	if a != Fingerprint("ws", "u1", "pizza artesanal", AspectSquare) {
		t.Fatal("whitespace changed the fingerprint")
	}
	for _, other := range []string{
		Fingerprint("ws", "u2", "pizza artesanal", AspectSquare),
		Fingerprint("ws2", "u1", "pizza artesanal", AspectSquare),
		Fingerprint("ws", "u1", "pizza artesanal", AspectStory),
		Fingerprint("ws", "u1", "Pizza artesanal", AspectSquare),
	} {
		if other == a {
			t.Fatal("different request shares a fingerprint")
		}
	}
	if len(a) != 64 {
		t.Fatalf("fingerprint %q is not a sha256 hex", a)
	}
}

func TestFieldBoundariesDoNotCollide(t *testing.T) {
	if Fingerprint("ab", "c", "x", AspectSquare) == Fingerprint("a", "bc", "x", AspectSquare) {
		t.Fatal("concatenation collision")
	}
}

func TestNewJobIsQueuedWithTheNormalizedPrompt(t *testing.T) {
	job, err := NewJob(Request{WorkspaceID: "ws", Prompt: "  pizza  ", Aspect: AspectPortrait}, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != StatusQueued || job.Prompt != "pizza" || job.RequestedBy != "u1" || job.Fingerprint != Fingerprint("ws", "u1", "pizza", AspectPortrait) {
		t.Fatalf("job %+v", job)
	}
	if !reflect.DeepEqual(job.Request(), Request{WorkspaceID: "ws", Prompt: "pizza", Aspect: AspectPortrait}) {
		t.Fatalf("request %+v", job.Request())
	}
}

func TestNewJobRejectsInvalidInput(t *testing.T) {
	if _, err := NewJob(Request{WorkspaceID: "ws", Prompt: "pizza", Aspect: AspectSquare}, " "); !errors.Is(err, ErrRequesterRequired) {
		t.Fatalf("missing requester: %v", err)
	}
	if _, err := NewJob(Request{WorkspaceID: "ws", Aspect: AspectSquare}, "u1"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("missing prompt: %v", err)
	}
}

func TestStaleCutoffIsTheActiveWindow(t *testing.T) {
	if got := ActiveSince(jobClock); !got.Equal(jobClock.Add(-10 * time.Minute)) {
		t.Fatalf("cutoff %s", got)
	}
}

func TestReferencesAreAPartOfTheFingerprintInOrder(t *testing.T) {
	plain := Fingerprint("ws", "u1", "pizza", AspectSquare)
	one := Fingerprint("ws", "u1", "pizza", AspectSquare, "m-1")
	two := Fingerprint("ws", "u1", "pizza", AspectSquare, "m-1", "m-2")
	swapped := Fingerprint("ws", "u1", "pizza", AspectSquare, "m-2", "m-1")
	seen := map[string]bool{}
	for _, fp := range []string{plain, one, two, swapped} {
		if seen[fp] {
			t.Fatal("different references share a fingerprint")
		}
		seen[fp] = true
	}
	if Fingerprint("ws", "u1", "pizza", AspectSquare, "m-1\x00m-2") == two {
		t.Fatal("reference boundary collision")
	}
}

func TestNewJobKeepsTheTrimmedReferences(t *testing.T) {
	job, err := NewJob(Request{WorkspaceID: "ws", Prompt: "pizza", Aspect: AspectSquare, ReferenceMediaIDs: []string{" m-1 ", "m-2"}}, "u1")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"m-1", "m-2"}
	if !reflect.DeepEqual(job.ReferenceMediaIDs, want) || !reflect.DeepEqual(job.Request().ReferenceMediaIDs, want) {
		t.Fatalf("job %+v", job)
	}
	if job.Fingerprint != Fingerprint("ws", "u1", "pizza", AspectSquare, want...) {
		t.Fatal("references left out of the fingerprint")
	}
}
