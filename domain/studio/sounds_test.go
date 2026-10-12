package studio

import (
	"encoding/json"
	"os"
	"testing"
)

type fixtureSound struct {
	ID         string `json:"id"`
	AssetID    string `json:"assetId"`
	DurationMS int64  `json:"durationMs"`
}

func TestBuiltinSoundsMatchTheEditorList(t *testing.T) {
	raw, err := os.ReadFile("testdata/studio_sounds.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Prefix string         `json:"prefix"`
		Sounds []fixtureSound `json:"sounds"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Prefix != SoundAssetPrefix {
		t.Fatalf("the editor prefixes sounds with %q, the backend with %q", fixture.Prefix, SoundAssetPrefix)
	}
	if len(fixture.Sounds) != len(BuiltinSounds()) {
		t.Fatalf("the editor lists %d sounds, the backend %d", len(fixture.Sounds), len(BuiltinSounds()))
	}
	for _, sound := range fixture.Sounds {
		got, ok := BuiltinSound(sound.AssetID)
		if !ok {
			t.Errorf("sound %s is missing from the backend", sound.ID)
			continue
		}
		if got != sound.DurationMS {
			t.Errorf("sound %s lasts %d ms, the editor says %d", sound.ID, got, sound.DurationMS)
		}
	}
}

func TestSoundAssetsOnlyBelongToAudioClips(t *testing.T) {
	cases := []struct {
		name    string
		clip    Clip
		invalid bool
	}{
		{"a known sound on an audio clip", Clip{Type: ClipAudio, AssetID: SoundAssetPrefix + "pop"}, false},
		{"an unknown sound", Clip{Type: ClipAudio, AssetID: SoundAssetPrefix + "nope"}, true},
		{"a sound on a video clip", Clip{Type: ClipVideo, AssetID: SoundAssetPrefix + "pop"}, true},
		{"a library media", Clip{Type: ClipAudio, AssetID: "6f1d3b8e-2c4a-4f1e-9a7b-0c1d2e3f4a5b"}, false},
	}
	for _, tc := range cases {
		if got := tc.clip.soundAssetIssue() != ""; got != tc.invalid {
			t.Errorf("%s: invalid %v, want %v", tc.name, got, tc.invalid)
		}
	}
}

func TestDocumentsWithBuiltinSoundsValidate(t *testing.T) {
	film := func(assetID string) []byte {
		doc := map[string]any{
			"schema": "studio.video", "version": 1,
			"canvas":     map[string]any{"aspect": "square", "background": "#000000"},
			"durationMs": 500,
			"tracks": []any{map[string]any{"id": "t-1", "kind": "audio", "clips": []any{map[string]any{
				"id": "c-1", "type": "audio", "assetId": assetID, "startMs": 0, "durationMs": 500, "trimInMs": 0, "volume": 1, "fadeInMs": 0, "fadeOutMs": 0,
				"transform": map[string]any{"x": 0.5, "y": 0.5, "w": 1, "h": 1, "rotation": 0, "opacity": 1},
			}}}},
		}
		raw, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	if err := ValidateDocument(KindVideo, film(SoundAssetPrefix+"whoosh")); err != nil {
		t.Fatalf("a document with a built-in sound was refused: %v", err)
	}
	if err := ValidateDocument(KindVideo, film(SoundAssetPrefix+"nope")); err == nil {
		t.Fatal("a document with an unknown built-in sound was accepted")
	}
}
