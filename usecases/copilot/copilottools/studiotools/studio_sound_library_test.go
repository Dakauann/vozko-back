package studiotools

import (
	"testing"

	"vozko/domain/copilot"
	"vozko/domain/studio/library"
)

func TestTheLibrarySearchFindsBuiltinSoundsOnlyForVideo(t *testing.T) {
	got := librarySearch(t, copilot.StudioVideo, "pt-BR", map[string]interface{}{"query": "plim", "category": library.CategoryAudio})
	if len(got.Items) == 0 || got.Items[0].ItemID != "sound-ding" || got.Items[0].Kind != library.KindClips || got.Items[0].Category != library.CategoryAudio {
		t.Fatalf("result = %+v", got)
	}
	image := librarySearch(t, copilot.StudioImage, "pt-BR", map[string]interface{}{"query": "plim"})
	if len(image.Items) != 0 {
		t.Fatalf("sounds must not be offered to the image editor: %+v", image.Items)
	}
}

func TestAddElementAcceptsABuiltinSoundInTheVideoEditor(t *testing.T) {
	ops := []studioVideoOperation{{Op: "add_element", studioElementArgs: studioElementArgs{ItemID: "sound-whoosh"}}}
	if err := checkElements(ops, copilot.StudioVideo); err != nil {
		t.Fatalf("a built-in sound was refused: %v", err)
	}
	images := []studioImageOperation{{Op: "add_element", studioElementArgs: studioElementArgs{ItemID: "sound-whoosh"}}}
	if err := checkElements(images, copilot.StudioImage); err == nil {
		t.Fatal("a sound was accepted in the image editor")
	}
}
