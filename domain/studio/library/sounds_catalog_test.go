package library

import "testing"

func TestBuiltinSoundsAreVideoClipsInTheAudioCategory(t *testing.T) {
	c := mustCatalog(t)
	if _, ok := c.Category(CategoryAudio); !ok {
		t.Fatal("the audio category is missing")
	}
	found := 0
	for _, item := range c.Items {
		if !item.Sound() {
			continue
		}
		found++
		if item.Kind != KindClips || item.Supports("image") || !item.Supports("video") {
			t.Errorf("sound %s must be a video clip item, got kind %s for %v", item.ID, item.Kind, item.Editors)
		}
	}
	if found == 0 {
		t.Fatal("no sound in the audio category")
	}
}

func TestSoundsAreFoundByLocalizedNamesAndEnglishWords(t *testing.T) {
	c := mustCatalog(t)
	if got := ids(c.Search(Query{Text: "plim", Category: CategoryAudio, Editor: "video", Locale: "pt"})); len(got) == 0 || got[0] != "sound-ding" {
		t.Fatalf("plim = %v", got)
	}
	if got := ids(c.Search(Query{Text: "bell", Category: CategoryAudio, Editor: "video", Locale: "pt"})); len(got) == 0 || got[0] != "sound-ding" {
		t.Fatalf("an English word must find the sound: %v", got)
	}
	if got := c.Search(Query{Text: "plim", Editor: "image", Locale: "pt"}); len(got) != 0 {
		t.Fatalf("sounds must not reach the image editor: %v", ids(got))
	}
}
