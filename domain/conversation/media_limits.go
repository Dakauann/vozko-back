package conversation

import (
	"sort"

	"vozko/domain/channel"
	"vozko/domain/shared"
)

type MediaLimit struct {
	Kind      channel.MediaKind `json:"kind"`
	MaxBytes  int64             `json:"max_bytes"`
	MIMETypes []string          `json:"mime_types"`
}

type MediaLimitsReader interface {
	MediaLimits(entryType shared.EntryType) ([]MediaLimit, bool)
}

func MediaLimitsOf(limits map[channel.MediaKind]channel.MediaLimit) []MediaLimit {
	out := make([]MediaLimit, 0, len(limits))
	for kind, limit := range limits {
		if limit.MaxBytes <= 0 {
			continue
		}
		types := append([]string{}, limit.MIMETypes...)
		sort.Strings(types)
		out = append(out, MediaLimit{Kind: kind, MaxBytes: limit.MaxBytes, MIMETypes: types})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	return out
}
