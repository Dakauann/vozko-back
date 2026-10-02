package advertising

import (
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

func TestImageRequestNeedsWorkspacePromptAndAspect(t *testing.T) {
	ok := ImageRequest{WorkspaceID: "ws", Prompt: "café com leite", Aspect: AspectSquare}
	if err := ok.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []ImageRequest{
		{Prompt: "x", Aspect: AspectSquare},
		{WorkspaceID: "ws", Prompt: "  ", Aspect: AspectSquare},
		{WorkspaceID: "ws", Prompt: strings.Repeat("a", 4001), Aspect: AspectSquare},
		{WorkspaceID: "ws", Prompt: "x", Aspect: "wide"},
	} {
		if bad.Validate() == nil {
			t.Fatalf("%+v accepted", bad)
		}
	}
}
