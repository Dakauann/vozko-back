package media_infra

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"vozko/domain/captions"
	"vozko/domain/mediagen"
	"vozko/domain/stt"
)

const (
	maxProcessingSourceBytes = 120 << 20
	processingModel          = "vozko/processing"
)

type DenoiseGenerator struct {
	client *http.Client
}

var _ mediagen.Generator = (*DenoiseGenerator)(nil)

func NewDenoiseGenerator(client *http.Client) *DenoiseGenerator {
	return &DenoiseGenerator{client: client}
}

func (g *DenoiseGenerator) Generate(ctx context.Context, _ mediagen.Request, sources []mediagen.Source) (*mediagen.Output, error) {
	raw, err := soleSource(ctx, g.client, sources, maxProcessingSourceBytes)
	if err != nil {
		return nil, err
	}
	cleaned, err := CleanAudio(ctx, raw)
	if err != nil {
		return nil, err
	}
	return &mediagen.Output{Bytes: cleaned, MIMEType: "audio/mp4", Model: processingModel}, nil
}

type CaptionsGenerator struct {
	client      *http.Client
	transcriber stt.SegmentTranscriber
	language    string
}

var _ mediagen.Generator = (*CaptionsGenerator)(nil)

const captionsTranscriptionBudget = 5 * time.Minute

func NewCaptionsGenerator(client *http.Client, transcriber stt.SegmentTranscriber, language string) *CaptionsGenerator {
	return &CaptionsGenerator{client: client, transcriber: transcriber, language: language}
}

func (g *CaptionsGenerator) Generate(ctx context.Context, _ mediagen.Request, sources []mediagen.Source) (*mediagen.Output, error) {
	raw, err := soleSource(ctx, g.client, sources, maxProcessingSourceBytes)
	if err != nil {
		return nil, err
	}
	speech, err := speechWAV(ctx, raw)
	if err != nil {
		return nil, err
	}
	transcribing, cancel := context.WithTimeout(ctx, captionsTranscriptionBudget)
	defer cancel()
	transcription, err := g.transcriber.TranscribeSegments(transcribing, speech, g.language)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", mediagen.ErrGenerationFailed, err)
	}
	cues := captions.Cues(transcription.TimedWords(), captions.SocialVideo)
	if len(cues) == 0 {
		return nil, fmt.Errorf("%w: no speech was found", mediagen.ErrGenerationFailed)
	}
	return &mediagen.Output{Bytes: []byte(webVTT(cues)), MIMEType: "text/vtt", Model: processingModel}, nil
}

func speechWAV(ctx context.Context, raw []byte) ([]byte, error) {
	dir, cleanup, err := workDir()
	if err != nil {
		return nil, err
	}
	defer cleanup()
	in, err := writeInput(dir, "source", raw)
	if err != nil {
		return nil, err
	}
	out := filepath.Join(dir, "speech.wav")
	if err := runFFmpegArgs(ctx, "-i", in, "-vn", "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", "-y", out); err != nil {
		return nil, err
	}
	return readOutput(out)
}

func webVTT(cues []captions.Cue) string {
	var b strings.Builder
	b.WriteString("WEBVTT\n")
	for i, cue := range cues {
		fmt.Fprintf(&b, "\n%d\n%s --> %s\n%s\n", i+1, vttTime(cue.Start), vttTime(cue.End), cue.Text())
	}
	return b.String()
}

func vttTime(seconds float64) string {
	d := time.Duration(seconds * float64(time.Second)).Round(time.Millisecond)
	return fmt.Sprintf("%02d:%02d:%02d.%03d", int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60, int(d.Milliseconds())%1000)
}
