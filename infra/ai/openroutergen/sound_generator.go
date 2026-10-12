package openroutergen

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"vozko/domain/mediagen"
)

const (
	soundFormat        = "mp3"
	generationIDHeader = "X-Generation-Id"
	soundInstruction   = "A sound effect only, with no speech, singing or background music, about %s seconds long."
)

type SoundEncoder interface {
	EncodeSound(ctx context.Context, raw []byte, length time.Duration) ([]byte, error)
}

type SoundGenerator struct {
	apiKey  string
	baseURL string
	http    *http.Client
	unsent  []time.Duration
	encoder SoundEncoder
}

var _ mediagen.Generator = (*SoundGenerator)(nil)

func NewSoundGenerator(cfg Config, encoder SoundEncoder) (*SoundGenerator, error) {
	if encoder == nil {
		return nil, fmt.Errorf("openroutergen: a sound encoder is required")
	}
	base, err := NewImageGenerator(cfg)
	if err != nil {
		return nil, err
	}
	return &SoundGenerator{apiKey: base.apiKey, baseURL: base.baseURL, http: base.http, unsent: base.unsent, encoder: encoder}, nil
}

type speechRequest struct {
	Model          string `json:"model"`
	Input          string `json:"input"`
	ResponseFormat string `json:"response_format"`
}

func (g *SoundGenerator) Generate(ctx context.Context, req mediagen.Request, _ []mediagen.Source) (*mediagen.Output, error) {
	if req.Kind != mediagen.KindSoundEffect {
		return nil, fmt.Errorf("openroutergen: %q is not a sound effect", req.Kind)
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	model := strings.TrimSpace(req.Model)
	raw, generationID, err := g.speak(ctx, speechRequest{Model: model, Input: soundInput(req), ResponseFormat: soundFormat})
	if err != nil {
		return nil, mediagen.Charged(generationID, err)
	}
	encoded, err := g.encoder.EncodeSound(ctx, raw, time.Duration(req.DurationMS)*time.Millisecond)
	if err != nil {
		return nil, mediagen.Charged(generationID, fmt.Errorf("%w: %v", mediagen.ErrGenerationFailed, err))
	}
	return &mediagen.Output{Bytes: encoded, MIMEType: "audio/mp4", Model: model, GenerationID: generationID}, nil
}

func soundInput(req mediagen.Request) string {
	seconds := strconv.FormatFloat(float64(req.DurationMS)/1000, 'f', -1, 64)
	return strings.TrimSpace(req.Prompt) + "\n\n" + fmt.Sprintf(soundInstruction, seconds)
}

func (g *SoundGenerator) speak(ctx context.Context, body speechRequest) ([]byte, string, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, "", err
	}
	resp, err := send(ctx, g.http, g.unsent, func() (*http.Request, error) {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+"/audio/speech", bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Authorization", "Bearer "+g.apiKey)
		httpReq.Header.Set("Content-Type", "application/json")
		return httpReq, nil
	})
	if err != nil {
		return nil, "", fmt.Errorf("openroutergen: request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", refusal(resp)
	}
	generationID := strings.TrimSpace(resp.Header.Get(generationIDHeader))
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxAudioBytes+1))
	switch {
	case err != nil:
		return nil, generationID, fmt.Errorf("%w: read audio: %v", mediagen.ErrGenerationFailed, err)
	case len(raw) == 0:
		return nil, generationID, fmt.Errorf("%w: the provider returned no audio", mediagen.ErrGenerationFailed)
	case len(raw) > maxAudioBytes:
		return nil, generationID, fmt.Errorf("%w: audio larger than %d bytes", mediagen.ErrGenerationFailed, maxAudioBytes)
	}
	return raw, generationID, nil
}
