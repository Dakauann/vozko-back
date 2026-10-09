package media_infra

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	media_domain "vozko/domain/media"
	"vozko/domain/mediagen"
	"vozko/domain/stt"
)

type fakeTranscriber struct {
	audio    []byte
	segments []stt.Segment
}

func (f *fakeTranscriber) TranscribeSegments(_ context.Context, audio []byte, _ string) (*stt.Transcription, error) {
	f.audio = audio
	return &stt.Transcription{Segments: f.segments}, nil
}

func talkSource(t *testing.T) (*http.Client, []mediagen.Source) {
	t.Helper()
	srv := mediaServer(t, map[string][]byte{
		"talk.mp4": synthesize(t, "talk.mp4", "-f", "lavfi", "-i", "testsrc=s=160x120:d=2", "-f", "lavfi", "-i", "sine=frequency=500:duration=2", "-pix_fmt", "yuv420p", "-shortest"),
	})
	return srv.Client(), []mediagen.Source{{MediaID: "talk", URL: srv.URL + "/talk.mp4", Type: media_domain.MediaTypeProductVideo}}
}

func TestCaptionsAreTheSpeechOfTheSourceAsWebVTT(t *testing.T) {
	requireFFmpeg(t)
	transcriber := &fakeTranscriber{segments: []stt.Segment{
		{Start: 0, End: 1.25, Text: " Olá, tudo bem? "},
		{Start: 1.3, End: 1.3, Text: "vazio"},
		{Start: 2, End: 8, Text: strings.Repeat("palavra ", 30)},
	}}
	client, sources := talkSource(t)
	result, err := NewCaptionsGenerator(client, transcriber, "pt").Generate(context.Background(), mediagen.Request{}, sources)
	if err != nil {
		t.Fatal(err)
	}
	vtt := string(result.Bytes)
	if !strings.HasPrefix(vtt, "WEBVTT\n") || !strings.Contains(vtt, "00:00:00.000 --> 00:00:01.250\nOlá, tudo bem?") || strings.Contains(vtt, "vazio") {
		t.Fatalf("vtt %s", vtt)
	}
	if strings.Count(vtt, "-->") < 3 || !strings.Contains(vtt, "--> 00:00:08.000") || result.MIMEType != "text/vtt" {
		t.Fatalf("a long segment is split into readable cues ending with it: %s", vtt)
	}
	if string(transcriber.audio[8:12]) != "WAVE" {
		t.Fatal("the transcriber gets the speech as WAV")
	}
}

func TestCaptionsWithoutSpeechFail(t *testing.T) {
	requireFFmpeg(t)
	client, sources := talkSource(t)
	if _, err := NewCaptionsGenerator(client, &fakeTranscriber{}, "pt").Generate(context.Background(), mediagen.Request{}, sources); err == nil {
		t.Fatal("empty captions were delivered")
	}
}

func TestDenoiseReturnsCleanedAudioFromAVideo(t *testing.T) {
	requireFFmpeg(t)
	client, sources := talkSource(t)
	out, err := NewDenoiseGenerator(client).Generate(context.Background(), mediagen.Request{}, sources)
	if err != nil {
		t.Fatal(err)
	}
	info := probe(t, out.Bytes, "stream=codec_type,codec_name")
	if !strings.Contains(info, "codec_name=aac") || strings.Contains(info, "codec_type=video") {
		t.Fatalf("info %s", info)
	}
}

func TestAProcessingJobNeedsExactlyOneSource(t *testing.T) {
	if _, err := NewDenoiseGenerator(http.DefaultClient).Generate(context.Background(), mediagen.Request{}, nil); err == nil {
		t.Fatal("processed without a source")
	}
}

type deadlineTranscriber struct {
	fakeTranscriber
	deadline time.Time
	bounded  bool
}

func (d *deadlineTranscriber) TranscribeSegments(ctx context.Context, audio []byte, language string) (*stt.Transcription, error) {
	d.deadline, d.bounded = ctx.Deadline()
	return d.fakeTranscriber.TranscribeSegments(ctx, audio, language)
}

func TestCaptionsGiveTheTranscriptionFiveMinutes(t *testing.T) {
	requireFFmpeg(t)
	transcriber := &deadlineTranscriber{fakeTranscriber: fakeTranscriber{segments: []stt.Segment{{Start: 0, End: 1, Text: "Olá"}}}}
	client, sources := talkSource(t)
	started := time.Now()
	if _, err := NewCaptionsGenerator(client, transcriber, "pt").Generate(context.Background(), mediagen.Request{}, sources); err != nil {
		t.Fatal(err)
	}
	budget := transcriber.deadline.Sub(started)
	if !transcriber.bounded || budget < 4*time.Minute+50*time.Second || budget > 5*time.Minute+time.Second {
		t.Fatalf("the transcription must get a five minute budget, got %v (bounded %v)", budget, transcriber.bounded)
	}
}
