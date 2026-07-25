package container

import (
	holdmusichttp "vozko/delivery/http/holdmusic"
	dialer_infra "vozko/infra/dialer"
)

type builtinHoldMusicCatalog struct{}

func (builtinHoldMusicCatalog) Tracks() []holdmusichttp.Track {
	src := dialer_infra.BuiltinHoldTracks()
	out := make([]holdmusichttp.Track, len(src))
	for i, t := range src {
		out[i] = holdmusichttp.Track{Key: t.Key, Label: t.Label}
	}
	return out
}

func (builtinHoldMusicCatalog) Path(key string) (string, bool) {
	return dialer_infra.BuiltinHoldTrackPath(key)
}
