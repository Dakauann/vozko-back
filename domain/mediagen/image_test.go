package mediagen

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestAspectsMatchMetaRecommendedSizes(t *testing.T) {
	cases := map[Aspect]Size{AspectSquare: {1080, 1080}, AspectPortrait: {1080, 1350}, AspectStory: {1080, 1920}, AspectLandscape: {1920, 1080}}
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
	ok := Request{Kind: KindImage, WorkspaceID: "ws", Model: testModel, Prompt: "café com leite", Aspect: AspectSquare}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := (Request{Kind: KindImage, Model: testModel, Prompt: "x", Aspect: AspectSquare}).Validate(); !errors.Is(err, ErrWorkspaceRequired) {
		t.Fatalf("missing workspace: %v", err)
	}
	cases := []struct {
		req   Request
		field string
		code  string
	}{
		{Request{Kind: KindImage, WorkspaceID: "ws", Model: testModel, Prompt: "  ", Aspect: AspectSquare}, FieldPrompt, CodeRequired},
		{Request{Kind: KindImage, WorkspaceID: "ws", Model: testModel, Prompt: strings.Repeat("a", 4001), Aspect: AspectSquare}, FieldPrompt, CodeTooLong},
		{Request{Kind: KindImage, WorkspaceID: "ws", Model: testModel, Prompt: "x", Aspect: "wide"}, FieldAspect, CodeUnknown},
		{Request{Kind: KindImage, WorkspaceID: "ws", Model: testModel, Prompt: "x"}, FieldAspect, CodeRequired},
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
	err := Request{Kind: KindImage, WorkspaceID: "ws", Model: testModel, Prompt: "", Aspect: "wide"}.Validate()
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
		req := Request{Kind: KindImage, WorkspaceID: "ws", Model: testModel, Prompt: "pizza", Aspect: AspectSquare, ReferenceMediaIDs: refs}
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
		err := Request{Kind: KindImage, WorkspaceID: "ws", Model: testModel, Prompt: "pizza", Aspect: AspectSquare, ReferenceMediaIDs: refs}.Validate()
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

const testModel = "google/gemini-3-pro-image"

func TestRequestNeedsAModel(t *testing.T) {
	err := Request{Kind: KindImage, WorkspaceID: "ws", Prompt: "pizza", Aspect: AspectSquare}.Validate()
	var invalid *ValidationError
	if !errors.As(err, &invalid) || invalid.Codes()[FieldModel] != CodeRequired {
		t.Fatalf("got %v", err)
	}
	if err := (Request{Kind: KindImage, WorkspaceID: "ws", Model: "  ", Prompt: "pizza", Aspect: AspectSquare}).Validate(); !errors.As(err, &invalid) || invalid.Codes()[FieldModel] != CodeRequired {
		t.Fatalf("blank model: %v", err)
	}
}

func TestContentIsCheckedBeforeTheModelIsChosen(t *testing.T) {
	if err := (Request{Kind: KindImage, WorkspaceID: "ws", Prompt: "pizza", Aspect: AspectSquare}).ValidateContent(); err != nil {
		t.Fatal(err)
	}
	err := Request{Kind: KindImage, WorkspaceID: "ws", Prompt: "", Aspect: AspectSquare}.ValidateContent()
	var invalid *ValidationError
	if !errors.As(err, &invalid) || invalid.Codes()[FieldPrompt] != CodeRequired || invalid.Codes()[FieldModel] != "" {
		t.Fatalf("got %v", err)
	}
}

func TestOnlyCatalogImageModelsAreAccepted(t *testing.T) {
	models := []Model{{ID: "openai/gpt-image-2", Name: "GPT Image 2"}, {ID: testModel, Name: "Gemini 3 Pro Image"}}
	if err := Supported(models, " "+testModel+" "); err != nil {
		t.Fatal(err)
	}
	err := Supported(models, "anthropic/claude-sonnet-4")
	var invalid *ValidationError
	if !errors.As(err, &invalid) || invalid.Codes()[FieldModel] != CodeUnknown {
		t.Fatalf("got %v", err)
	}
	if err := Supported(nil, testModel); !errors.As(err, &invalid) {
		t.Fatalf("an empty catalog accepted %v", err)
	}
}
