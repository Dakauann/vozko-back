package dialer

import (
	"os"
	"path/filepath"
	"testing"
)

// The builtin catalog must decode end to end with the real shipped assets: a
// track that ships is a track that plays. Skips only when the assets directory
// is absent (e.g. a stripped CI checkout).
func TestBuiltinHoldTracks_ShippedAssetsDecode(t *testing.T) {
	dir := filepath.Join("..", "..", "assets", "sounds")
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("assets dir not present: %v", err)
	}
	t.Setenv("HOLD_MUSIC_ASSETS_DIR", dir)

	// Reset the process cache so this test exercises the real load path even if
	// another test touched it.
	builtinPCMMu.Lock()
	builtinPCMCache = map[string][]byte{}
	builtinPCMMu.Unlock()

	tracks := BuiltinHoldTracks()
	if len(tracks) != 5 {
		t.Fatalf("catalog size = %d, want the 5 shipped tracks", len(tracks))
	}
	for _, tr := range tracks {
		if tr.Key == "keyboard" || tr.file == "keyboard_sound.mp3" {
			t.Fatalf("the keyboard ambience must never be a hold option")
		}
		pcm := BuiltinHoldTrackPCM(tr.Key)
		if len(pcm) < sipBytesPerFrame*50 { // at least one second of audio
			t.Fatalf("track %q decoded to %d bytes; want a real loop", tr.Key, len(pcm))
		}
	}
	// Cached: a second read returns the same buffer without re-decoding.
	again := BuiltinHoldTrackPCM("lofi")
	if len(again) == 0 {
		t.Fatal("cached read failed")
	}
	if BuiltinHoldTrackPCM("does_not_exist") != nil {
		t.Fatal("unknown keys must yield nil")
	}
}
