package dialer

import (
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	voip_audio "vozko/infra/voip/audio"
)

// BuiltinHoldTrack is one of the ready to use hold music tracks shipped with the
// server (assets/sounds). The keyboard ambience is deliberately NOT in this
// catalog: it is the typing bed for AI calls, never a hold option.
type BuiltinHoldTrack struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	file  string
}

// The shipped tracks are compact mono MP3s (converted from the 33MB source wavs
// at 22.05kHz 64kbps, ~1.4MB each) so the image stays small and the same MP3
// loader used for HOLD_MUSIC_PATH and uploads decodes them.
var builtinHoldTracks = []BuiltinHoldTrack{
	{Key: "acoustic", Label: "Acústica", file: "acoustic_calm_hold_music.mp3"},
	{Key: "bossa_nova", Label: "Bossa Nova", file: "bossa_nova_calm_hold_music.mp3"},
	{Key: "jazz", Label: "Jazz", file: "jazz_calm_hold_music.mp3"},
	{Key: "lofi", Label: "Lo fi", file: "lofi_calm_hold_music.mp3"},
	{Key: "piano", Label: "Piano", file: "piano_calm_hold_music.mp3"},
}

// BuiltinHoldTracks returns the selectable catalog (key + display label).
func BuiltinHoldTracks() []BuiltinHoldTrack {
	out := make([]BuiltinHoldTrack, len(builtinHoldTracks))
	copy(out, builtinHoldTracks)
	return out
}

// IsBuiltinHoldTrack reports whether key names a shipped track.
func IsBuiltinHoldTrack(key string) bool {
	_, ok := builtinByKey(key)
	return ok
}

// BuiltinHoldTrackPath resolves the on-disk file for a catalog key (used by the
// preview stream endpoint). The catalog lookup is the traversal guard: only
// known keys map to files.
func BuiltinHoldTrackPath(key string) (string, bool) {
	t, ok := builtinByKey(key)
	if !ok {
		return "", false
	}
	return filepath.Join(builtinHoldAssetsDir(), t.file), true
}

func builtinByKey(key string) (BuiltinHoldTrack, bool) {
	for _, t := range builtinHoldTracks {
		if t.Key == key {
			return t, true
		}
	}
	return BuiltinHoldTrack{}, false
}

func builtinHoldAssetsDir() string {
	if dir := strings.TrimSpace(os.Getenv("HOLD_MUSIC_ASSETS_DIR")); dir != "" {
		return dir
	}
	return filepath.Join("assets", "sounds")
}

// Decoded telephony PCM per builtin, loaded lazily ONCE per process and shared
// read-only (the same boot-cache discipline as the keyboard bed and
// HOLD_MUSIC_PATH). A missing/corrupt asset caches nil and logs once; holds then
// use the fallback chain instead of erroring per call.
var (
	builtinPCMMu    sync.Mutex
	builtinPCMCache = map[string][]byte{}
)

// BuiltinHoldTrackPCM returns the decoded 8kHz mono PCM loop for a builtin key,
// or nil when the key is unknown or its asset cannot be decoded.
func BuiltinHoldTrackPCM(key string) []byte {
	if _, ok := builtinByKey(key); !ok {
		return nil
	}
	builtinPCMMu.Lock()
	defer builtinPCMMu.Unlock()
	if pcm, ok := builtinPCMCache[key]; ok {
		return pcm
	}
	path, _ := BuiltinHoldTrackPath(key)
	pcm, err := voip_audio.LoadMP3AsTelephonyPCM(path)
	if err != nil {
		log.Printf("[HoldMusic] builtin track %q unusable (%v); callers get the fallback audio", key, err)
		pcm = nil
	}
	builtinPCMCache[key] = pcm
	return pcm
}
