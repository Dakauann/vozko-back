package mediagen

import (
	"errors"
	"reflect"
	"strings"
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
	a := imageFingerprint("ws", "u1", testModel, "  pizza   artesanal\n", AspectSquare)
	if a != imageFingerprint("ws", "u1", testModel, "pizza artesanal", AspectSquare) {
		t.Fatal("whitespace changed the fingerprint")
	}
	for _, other := range []string{
		imageFingerprint("ws", "u2", testModel, "pizza artesanal", AspectSquare),
		imageFingerprint("ws2", "u1", testModel, "pizza artesanal", AspectSquare),
		imageFingerprint("ws", "u1", testModel, "pizza artesanal", AspectStory),
		imageFingerprint("ws", "u1", testModel, "Pizza artesanal", AspectSquare),
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
	if imageFingerprint("ab", "c", testModel, "x", AspectSquare) == imageFingerprint("a", "bc", testModel, "x", AspectSquare) {
		t.Fatal("concatenation collision")
	}
}

func TestNewJobIsQueuedWithTheNormalizedPrompt(t *testing.T) {
	job, err := NewJob(Request{Kind: KindImage, WorkspaceID: "ws", Model: testModel, Prompt: "  pizza  ", Aspect: AspectPortrait}, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != StatusQueued || job.Prompt != "pizza" || job.RequestedBy != "u1" || job.Fingerprint != imageFingerprint("ws", "u1", testModel, "pizza", AspectPortrait) {
		t.Fatalf("job %+v", job)
	}
	if !reflect.DeepEqual(job.Request(), Request{Kind: KindImage, WorkspaceID: "ws", Model: testModel, Prompt: "pizza", Aspect: AspectPortrait}) {
		t.Fatalf("request %+v", job.Request())
	}
}

func TestNewJobRejectsInvalidInput(t *testing.T) {
	if _, err := NewJob(Request{Kind: KindImage, WorkspaceID: "ws", Model: testModel, Prompt: "pizza", Aspect: AspectSquare}, " "); !errors.Is(err, ErrRequesterRequired) {
		t.Fatalf("missing requester: %v", err)
	}
	if _, err := NewJob(Request{Kind: KindImage, WorkspaceID: "ws", Model: testModel, Aspect: AspectSquare}, "u1"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("missing prompt: %v", err)
	}
}

func TestStaleCutoffIsTheActiveWindow(t *testing.T) {
	if got := ActiveSince(jobClock); !got.Equal(jobClock.Add(-10 * time.Minute)) {
		t.Fatalf("cutoff %s", got)
	}
}

func TestReferencesAreAPartOfTheFingerprintInOrder(t *testing.T) {
	plain := imageFingerprint("ws", "u1", testModel, "pizza", AspectSquare)
	one := imageFingerprint("ws", "u1", testModel, "pizza", AspectSquare, "m-1")
	two := imageFingerprint("ws", "u1", testModel, "pizza", AspectSquare, "m-1", "m-2")
	swapped := imageFingerprint("ws", "u1", testModel, "pizza", AspectSquare, "m-2", "m-1")
	seen := map[string]bool{}
	for _, fp := range []string{plain, one, two, swapped} {
		if seen[fp] {
			t.Fatal("different references share a fingerprint")
		}
		seen[fp] = true
	}
	if imageFingerprint("ws", "u1", testModel, "pizza", AspectSquare, "m-1\x00m-2") == two {
		t.Fatal("reference boundary collision")
	}
}

func TestNewJobKeepsTheTrimmedReferences(t *testing.T) {
	job, err := NewJob(Request{Kind: KindImage, WorkspaceID: "ws", Model: testModel, Prompt: "pizza", Aspect: AspectSquare, ReferenceMediaIDs: []string{" m-1 ", "m-2"}}, "u1")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"m-1", "m-2"}
	if !reflect.DeepEqual(job.ReferenceMediaIDs, want) || !reflect.DeepEqual(job.Request().ReferenceMediaIDs, want) {
		t.Fatalf("job %+v", job)
	}
	if job.Fingerprint != imageFingerprint("ws", "u1", testModel, "pizza", AspectSquare, want...) {
		t.Fatal("references left out of the fingerprint")
	}
}

func TestTheModelIsPartOfTheJobAndItsFingerprint(t *testing.T) {
	job, err := NewJob(Request{Kind: KindImage, WorkspaceID: "ws", Model: " " + testModel + " ", Prompt: "pizza", Aspect: AspectSquare}, "u1")
	if err != nil {
		t.Fatal(err)
	}
	if job.Model != testModel || job.Request().Model != testModel {
		t.Fatalf("job %+v", job)
	}
	if imageFingerprint("ws", "u1", testModel, "pizza", AspectSquare) == imageFingerprint("ws", "u1", "openai/gpt-image-2", "pizza", AspectSquare) {
		t.Fatal("different models share a fingerprint")
	}
}

func imageFingerprint(workspaceID, requestedBy, model, prompt string, aspect Aspect, references ...string) string {
	return Request{Kind: KindImage, WorkspaceID: workspaceID, Model: model, Prompt: prompt, Aspect: aspect, ReferenceMediaIDs: references}.Fingerprint(requestedBy)
}

func TestAJobKeepsTheReferenceItIsChargedTo(t *testing.T) {
	req := Request{Kind: KindMusic, WorkspaceID: "ws-1", Model: "m", Prompt: "samba", BillingReference: "aichat:th-1"}
	job, err := NewJob(req, "u-1")
	if err != nil {
		t.Fatal(err)
	}
	if job.BillingReference != "aichat:th-1" {
		t.Fatalf("the job must keep its charge reference, got %q", job.BillingReference)
	}
	plain := req
	plain.BillingReference = ""
	if req.Fingerprint("u-1") != plain.Fingerprint("u-1") {
		t.Fatal("the charge reference must not change what counts as the same generation")
	}
}

func TestOutcomeShowsAFailureThatIsStillReconcilingItsCost(t *testing.T) {
	cases := []struct {
		job  Job
		want Status
	}{
		{Job{Status: StatusSettling, FailureCode: FailureGeneration}, StatusFailed},
		{Job{Status: StatusSettling, MediaID: "m-1"}, StatusSettling},
		{Job{Status: StatusRunning}, StatusRunning},
		{Job{Status: StatusDone, MediaID: "m-1"}, StatusDone},
	}
	for _, c := range cases {
		if got := c.job.Outcome(); got != c.want {
			t.Fatalf("job %+v outcome %s, want %s", c.job, got, c.want)
		}
	}
}

func TestFailureDetailIsBounded(t *testing.T) {
	if FailureDetail(nil) != "" {
		t.Fatal("no cause, no detail")
	}
	long := errors.New(strings.Repeat("é", maxFailureDetail+50))
	if got := []rune(FailureDetail(long)); len(got) != maxFailureDetail {
		t.Fatalf("detail has %d runes", len(got))
	}
}
