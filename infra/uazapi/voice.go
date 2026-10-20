package uazapi

import (
	"context"
	"fmt"

	"vozko/domain/media"
	media_infra "vozko/infra/media"
)

const maxVoiceBytes = 32 << 20

type voiceTranscoder struct {
	downloads media.RemoteFiles
}

func NewVoiceTranscoder(downloads media.RemoteFiles) *voiceTranscoder {
	return &voiceTranscoder{downloads: downloads}
}

func (t *voiceTranscoder) ToVoiceNote(ctx context.Context, url string) ([]byte, error) {
	file, err := media.Download(ctx, t.downloads, url, maxVoiceBytes)
	if err != nil {
		return nil, fmt.Errorf("unofficial whatsapp: could not fetch the audio: %w", err)
	}

	converted, err := media_infra.ConvertToOGGOpus(file.Data)
	if err != nil {
		return nil, fmt.Errorf("unofficial whatsapp: could not convert the audio: %w", err)
	}
	return converted, nil
}
