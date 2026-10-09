package media_infra

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"vozko/domain/mediagen"
	"vozko/domain/stt"
)

const (
	maxProcessingSourceBytes = 120 << 20
	maxCueRunes              = 84
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
	vtt, ok := webVTT(transcription.Segments)
	if !ok {
		return nil, fmt.Errorf("%w: no speech was found", mediagen.ErrGenerationFailed)
	}
	return &mediagen.Output{Bytes: []byte(vtt), MIMEType: "text/vtt", Model: processingModel}, nil
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

func webVTT(segments []stt.Segment) (string, bool) {
	var b strings.Builder
	b.WriteString("WEBVTT\n")
	cues := 0
	for _, seg := range segments {
		text := strings.Join(strings.Fields(seg.Text), " ")
		if text == "" || seg.End <= seg.Start {
			continue
		}
		for _, cue := range splitCue(text, seg.Start, seg.End) {
			cues++
			fmt.Fprintf(&b, "\n%d\n%s --> %s\n%s\n", cues, vttTime(cue.start), vttTime(cue.end), cue.text)
		}
	}
	return b.String(), cues > 0
}

type cue struct {
	start, end float64
	text       string
}

func splitCue(text string, start, end float64) []cue {
	var chunks []string
	var line []string
	length := 0
	for _, word := range strings.Fields(text) {
		if length > 0 && length+1+len([]rune(word)) > maxCueRunes {
			chunks = append(chunks, strings.Join(line, " "))
			line, length = nil, 0
		}
		if length > 0 {
			length++
		}
		line = append(line, word)
		length += len([]rune(word))
	}
	chunks = append(chunks, strings.Join(line, " "))
	total := 0
	for _, c := range chunks {
		total += len([]rune(c))
	}
	out := make([]cue, 0, len(chunks))
	at := start
	for i, c := range chunks {
		next := end
		if i < len(chunks)-1 {
			next = at + (end-start)*float64(len([]rune(c)))/float64(total)
		}
		out = append(out, cue{start: at, end: next, text: c})
		at = next
	}
	return out
}

func vttTime(seconds float64) string {
	d := time.Duration(seconds * float64(time.Second)).Round(time.Millisecond)
	return fmt.Sprintf("%02d:%02d:%02d.%03d", int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60, int(d.Milliseconds())%1000)
}
