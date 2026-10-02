package imagegen

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestAspectsMatchMetaRecommendedSizes(t *testing.T) {
	cases := map[Aspect]Size{AspectSquare: {1080, 1080}, AspectPortrait: {1080, 1350}, AspectStory: {1080, 1920}}
	for aspect, want := range cases {
		got, err := aspect.Size()
		if err != nil || got != want {
			t.Fatalf("%s: %+v %v", aspect, got, err)
		}
	}
	if _, err := Aspect("panorama").Size(); err == nil {
		t.Fatal("unknown aspect accepted")
	}
}

func TestRequestNeedsWorkspacePromptAndAspect(t *testing.T) {
	ok := Request{WorkspaceID: "ws", Prompt: "café com leite", Aspect: AspectSquare}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (Request{Prompt: "x", Aspect: AspectSquare}).Validate(); !errors.Is(err, ErrWorkspaceRequired) {
		t.Fatalf("missing workspace: %v", err)
	}
	cases := []struct {
		req   Request
		field string
		code  string
	}{
		{Request{WorkspaceID: "ws", Prompt: "  ", Aspect: AspectSquare}, FieldPrompt, CodeRequired},
		{Request{WorkspaceID: "ws", Prompt: strings.Repeat("a", 4001), Aspect: AspectSquare}, FieldPrompt, CodeTooLong},
		{Request{WorkspaceID: "ws", Prompt: "x", Aspect: "wide"}, FieldAspect, CodeUnknown},
		{Request{WorkspaceID: "ws", Prompt: "x"}, FieldAspect, CodeRequired},
	}
	for _, c := range cases {
		err := c.req.Validate()
		var invalid *ValidationError
		if !errors.As(err, &invalid) || !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("%+v: %v", c.req, err)
		}
		if got := invalid.Codes()[c.field]; got != c.code {
			t.Fatalf("%+v: %s = %q, want %q", c.req, c.field, got, c.code)
		}
	}
}

func TestEveryInvalidFieldIsReportedAtOnce(t *testing.T) {
	err := Request{WorkspaceID: "ws", Prompt: "", Aspect: "wide"}.Validate()
	var invalid *ValidationError
	if !errors.As(err, &invalid) || len(invalid.Codes()) != 2 {
		t.Fatalf("got %v", err)
	}
}

func referenceIDs(n int) []string {
	ids := make([]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("m-%d", i)
	}
	return ids
}

func TestReferenceImagesAreOptional(t *testing.T) {
	for _, refs := range [][]string{nil, {}, referenceIDs(MaxReferenceImages)} {
		req := Request{WorkspaceID: "ws", Prompt: "pizza", Aspect: AspectSquare, ReferenceMediaIDs: refs}
		if err := req.Validate(); err != nil {
			t.Fatalf("%d references: %v", len(refs), err)
		}
	}
}

func TestReferenceImagesMustBeFewNonEmptyAndUnique(t *testing.T) {
	cases := map[string][]string{
		CodeTooMany:   referenceIDs(MaxReferenceImages + 1),
		CodeRequired:  {"m-1", "  "},
		CodeDuplicate: {"m-1", "m-2", " m-1 "},
	}
	for code, refs := range cases {
		err := Request{WorkspaceID: "ws", Prompt: "pizza", Aspect: AspectSquare, ReferenceMediaIDs: refs}.Validate()
		var invalid *ValidationError
		if !errors.As(err, &invalid) || invalid.Codes()[FieldReferences] != code {
			t.Fatalf("%v: got %v, want %s", refs, err, code)
		}
	}
}

func TestTheReferenceLimitIsTheConfiguredModelLimit(t *testing.T) {
	if MaxReferenceImages != 16 {
		t.Fatalf("limit %d", MaxReferenceImages)
	}
}
