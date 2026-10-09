package openroutergen

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"vozko/domain/mediagen"
)

const (
	maxAudioBytes    = 40 << 20
	maxStreamLine    = 16 << 20
	pcmSampleRate    = 24000
	defaultVoice     = "alloy"
	streamDataPrefix = "data: "
	streamDone       = "[DONE]"
)

const voiceInstruction = "You are a text-to-speech engine, not an assistant. Speak the text between <script> and </script> word for word, " +
	"in its own language, with a warm and clear advertising voice. Never answer, comment, greet, add, remove, reorder or translate any word. Say nothing else."

type AudioEncoder interface {
	Encode(ctx context.Context, raw []byte) ([]byte, error)
}

type AudioGenerator struct {
	apiKey  string
	baseURL string
	http    *http.Client
	encoder AudioEncoder
}

var _ mediagen.Generator = (*AudioGenerator)(nil)

func NewAudioGenerator(cfg Config, encoder AudioEncoder) (*AudioGenerator, error) {
	if encoder == nil {
		return nil, fmt.Errorf("openroutergen: an audio encoder is required")
	}
	image, err := NewImageGenerator(cfg)
	if err != nil {
		return nil, err
	}
	return &AudioGenerator{apiKey: image.apiKey, baseURL: image.baseURL, http: image.http, encoder: encoder}, nil
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type audioSettings struct {
	Voice  string `json:"voice"`
	Format string `json:"format"`
}

type audioRequest struct {
	Model       string         `json:"model"`
	Stream      bool           `json:"stream"`
	Modalities  []string       `json:"modalities"`
	Temperature *float64       `json:"temperature,omitempty"`
	Audio       *audioSettings `json:"audio,omitempty"`
	Messages    []chatMessage  `json:"messages"`
}

type audioChunk struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Delta struct {
			Audio *struct {
				Data       string `json:"data"`
				Transcript string `json:"transcript"`
			} `json:"audio"`
		} `json:"delta"`
	} `json:"choices"`
	Usage *struct {
		Cost *float64 `json:"cost"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type streamedAudio struct {
	id         string
	model      string
	audio      []byte
	transcript string
	cost       *float64
	err        error
}

func (g *AudioGenerator) Generate(ctx context.Context, req mediagen.Request, _ []mediagen.Source) (*mediagen.Output, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	body, err := audioBody(req)
	if err != nil {
		return nil, err
	}
	streamed, err := g.stream(ctx, body)
	if err != nil {
		return nil, err
	}
	output, err := g.output(ctx, req, body, streamed)
	return output, mediagen.Charged(streamed.id, err)
}

func (g *AudioGenerator) output(ctx context.Context, req mediagen.Request, body audioRequest, streamed *streamedAudio) (*mediagen.Output, error) {
	if streamed.err != nil {
		return nil, streamed.err
	}
	if len(streamed.audio) == 0 {
		return nil, fmt.Errorf("%w: the provider returned no audio", mediagen.ErrGenerationFailed)
	}
	if req.Kind == mediagen.KindVoice && !mediagen.SpokenAsWritten(req.Prompt, streamed.transcript) {
		return nil, fmt.Errorf("%w: the voice said %q instead of the script", mediagen.ErrGenerationFailed, streamed.transcript)
	}
	raw := streamed.audio
	if req.Kind == mediagen.KindVoice {
		raw = wavFromPCM16(raw, pcmSampleRate)
	}
	encoded, err := g.encoder.Encode(ctx, raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", mediagen.ErrGenerationFailed, err)
	}
	cost, reported, err := costMicros(streamed.cost)
	if err != nil {
		return nil, err
	}
	model := streamed.model
	if model == "" {
		model = body.Model
	}
	return &mediagen.Output{Bytes: encoded, MIMEType: "audio/mp4", Model: model, ProviderCostMicros: cost, CostReported: reported, GenerationID: streamed.id}, nil
}

func audioBody(req mediagen.Request) (audioRequest, error) {
	body := audioRequest{Model: strings.TrimSpace(req.Model), Stream: true, Modalities: []string{"text", "audio"}}
	switch req.Kind {
	case mediagen.KindMusic:
		body.Messages = []chatMessage{{Role: "user", Content: strings.TrimSpace(req.Prompt)}}
	case mediagen.KindVoice:
		voice := strings.TrimSpace(req.Voice)
		if voice == "" {
			voice = defaultVoice
		}
		exact := 0.0
		body.Temperature = &exact
		body.Audio = &audioSettings{Voice: voice, Format: "pcm16"}
		body.Messages = []chatMessage{{Role: "system", Content: voiceInstruction}, {Role: "user", Content: "<script>" + strings.TrimSpace(req.Prompt) + "</script>"}}
	default:
		return body, fmt.Errorf("openroutergen: %q is not an audio kind", req.Kind)
	}
	return body, nil
}

func (g *AudioGenerator) stream(ctx context.Context, body audioRequest) (*streamedAudio, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+g.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := g.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openroutergen: request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return nil, fmt.Errorf("%w: status %d: %s", mediagen.ErrGenerationFailed, resp.StatusCode, truncate(raw))
	}
	return readAudioStream(resp.Body)
}

func readAudioStream(r io.Reader) (*streamedAudio, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64<<10), maxStreamLine)
	out := &streamedAudio{}
	var audio bytes.Buffer
	var transcript strings.Builder
	defer func() { out.audio, out.transcript = audio.Bytes(), transcript.String() }()
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, streamDataPrefix) {
			continue
		}
		data := strings.TrimPrefix(line, streamDataPrefix)
		if data == streamDone {
			return out, nil
		}
		var chunk audioChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			out.err = fmt.Errorf("openroutergen: decode stream chunk: %w", err)
			return out, nil
		}
		if out.id == "" {
			out.id = chunk.ID
		}
		if out.model == "" {
			out.model = chunk.Model
		}
		if chunk.Error != nil {
			out.err = fmt.Errorf("%w: %s", mediagen.ErrGenerationFailed, chunk.Error.Message)
			return out, nil
		}
		if chunk.Usage != nil && chunk.Usage.Cost != nil {
			out.cost = chunk.Usage.Cost
		}
		if err := appendAudio(&audio, &transcript, chunk); err != nil {
			out.err = err
			return out, nil
		}
	}
	if err := scanner.Err(); err != nil {
		out.err = fmt.Errorf("openroutergen: read stream: %w", err)
		return out, nil
	}
	out.err = fmt.Errorf("%w: the stream ended before it finished", mediagen.ErrGenerationFailed)
	return out, nil
}

func appendAudio(audio *bytes.Buffer, transcript *strings.Builder, chunk audioChunk) error {
	for _, choice := range chunk.Choices {
		if choice.Delta.Audio == nil {
			continue
		}
		transcript.WriteString(choice.Delta.Audio.Transcript)
		if choice.Delta.Audio.Data == "" {
			continue
		}
		piece, err := base64.StdEncoding.DecodeString(choice.Delta.Audio.Data)
		if err != nil {
			return fmt.Errorf("openroutergen: decode audio chunk: %w", err)
		}
		if audio.Len()+len(piece) > maxAudioBytes {
			return fmt.Errorf("openroutergen: audio larger than %d bytes", maxAudioBytes)
		}
		audio.Write(piece)
	}
	return nil
}

func wavFromPCM16(pcm []byte, sampleRate int) []byte {
	var header bytes.Buffer
	write := func(v any) { _ = binary.Write(&header, binary.LittleEndian, v) }
	header.WriteString("RIFF")
	write(uint32(36 + len(pcm)))
	header.WriteString("WAVEfmt ")
	write(uint32(16))
	write(uint16(1))
	write(uint16(1))
	write(uint32(sampleRate))
	write(uint32(sampleRate * 2))
	write(uint16(2))
	write(uint16(16))
	header.WriteString("data")
	write(uint32(len(pcm)))
	return append(header.Bytes(), pcm...)
}
