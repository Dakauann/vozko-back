package uazapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	media_infra "vozko/infra/media"
)

// maxVoiceBytes bounds what will be pulled into memory to transcode.
//
// Well above any real voice note and well below anything that could exhaust the
// process: an operator sending an hour-long recording should get a refusal, not
// an OOM that takes the whole channel down with it.
const maxVoiceBytes = 32 << 20

// voiceTranscoder fetches a stored audio file and re-encodes it as a voice note.
//
// It lives in this package rather than in infra/media because it is the vendor
// half of the job — the caller hands it the URL our own storage produced, and it
// returns exactly the bytes this provider's send endpoint wants.
type voiceTranscoder struct {
	http *http.Client
}

// NewVoiceTranscoder builds the transcoder used by the send path.
func NewVoiceTranscoder() *voiceTranscoder {
	return &voiceTranscoder{
		// Its own client with its own timeout: transcoding is off the request
		// path, but a stalled media fetch must not hold a send worker forever.
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

	// LimitReader with one byte of headroom, so an oversized file is detected
	// rather than silently truncated into a corrupt recording.
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
