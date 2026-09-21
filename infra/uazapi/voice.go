package uazapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	media_infra "vozko/infra/media"
)

const maxVoiceBytes = 32 << 20

type voiceTranscoder struct {
	http *http.Client
}

func NewVoiceTranscoder() *voiceTranscoder {
	return &voiceTranscoder{
		http: &http.Client{Timeout: 60 * time.Second},
	}
}

func (t *voiceTranscoder) ToVoiceNote(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("unofficial whatsapp: bad media url: %w", err)
	}

	resp, err := t.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("unofficial whatsapp: could not fetch the audio: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unofficial whatsapp: audio fetch returned %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxVoiceBytes+1))
	if err != nil {
		return nil, fmt.Errorf("unofficial whatsapp: could not read the audio: %w", err)
	}
	if len(raw) > maxVoiceBytes {
		return nil, fmt.Errorf("unofficial whatsapp: audio exceeds the %d byte limit", maxVoiceBytes)
	}

	converted, err := media_infra.ConvertToOGGOpus(raw)
	if err != nil {
		return nil, fmt.Errorf("unofficial whatsapp: could not convert the audio: %w", err)
	}
	return converted, nil
}
