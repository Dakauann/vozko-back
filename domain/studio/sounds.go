package studio

import (
	"sort"
	"strings"
)

const SoundAssetPrefix = "sound:"

var builtinSounds = map[string]int64{
	"message":        1500,
	"call-connected": 660,
	"call-ended":     880,
	"incoming-call":  8700,
	"pop":            280,
	"click":          120,
	"whoosh":         750,
	"ding":           1400,
}

func BuiltinSounds() []string {
	out := make([]string, 0, len(builtinSounds))
	for id := range builtinSounds {
		out = append(out, SoundAssetPrefix+id)
	}
	sort.Strings(out)
	return out
}

func BuiltinSound(assetID string) (int64, bool) {
	if !strings.HasPrefix(assetID, SoundAssetPrefix) {
		return 0, false
	}
	durationMS, ok := builtinSounds[strings.TrimPrefix(assetID, SoundAssetPrefix)]
	return durationMS, ok
}

func (c Clip) soundAssetIssue() string {
	assetID := strings.TrimSpace(c.AssetID)
	if !strings.HasPrefix(assetID, SoundAssetPrefix) {
		return ""
	}
	if _, ok := BuiltinSound(assetID); ok && c.Type == ClipAudio {
		return ""
	}
	return CodeInvalid
}
