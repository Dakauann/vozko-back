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

func TestCaptionsFollowTheSentencesNotTheTranscriberSegments(t *testing.T) {
	requireFFmpeg(t)
	transcriber := &fakeTranscriber{segments: []stt.Segment{
		{Start: 0, End: 2.9, Text: "Oi gente, tudo bem? Hoje eu vou mostrar", Words: []stt.Word{
			{Text: "Oi", Start: 0, End: 0.2}, {Text: "gente,", Start: 0.22, End: 0.6}, {Text: "tudo", Start: 0.7, End: 0.95}, {Text: "bem?", Start: 0.97, End: 1.3},
			{Text: "Hoje", Start: 1.75, End: 2.05}, {Text: "eu", Start: 2.07, End: 2.2}, {Text: "vou", Start: 2.22, End: 2.4}, {Text: "mostrar", Start: 2.42, End: 2.9},
		}},
		{Start: 2.9, End: 2.9, Text: "vazio"},
		{Start: 2.9, End: 5.2, Text: "para vocês o nosso novo aplicativo.", Words: []stt.Word{
			{Text: "para", Start: 2.92, End: 3.1}, {Text: "vocês", Start: 3.12, End: 3.5}, {Text: "o", Start: 3.52, End: 3.6}, {Text: "nosso", Start: 3.62, End: 3.95},
			{Text: "novo", Start: 3.97, End: 4.25}, {Text: "aplicativo.", Start: 4.27, End: 5.2},
		}},
	}}
	client, sources := talkSource(t)
	result, err := NewCaptionsGenerator(client, transcriber, "pt").Generate(context.Background(), mediagen.Request{}, sources)
	if err != nil {
		t.Fatal(err)
	}
	vtt := string(result.Bytes)
	if !strings.HasPrefix(vtt, "WEBVTT\n") || result.MIMEType != "text/vtt" || strings.Contains(vtt, "vazio") {
		t.Fatalf("vtt %s", vtt)
	}
	if !strings.Contains(vtt, "\n00:00:00.000 --> 00:00:01.300\nOi gente, tudo bem?\n") || strings.Contains(vtt, "bem? Hoje") {
		t.Fatalf("the first sentence is not a caption of its own: %s", vtt)
	}
	if !strings.Contains(vtt, "\n00:00:01.750 --> ") || strings.Contains(vtt, "mostrar\n\n") {
		t.Fatalf("the second sentence does not start at its first word or breaks at the transcriber segment: %s", vtt)
	}
	if !strings.Contains(vtt, " --> 00:00:05.200\n") {
		t.Fatalf("the last caption does not end with the speech: %s", vtt)
	}
	if string(transcriber.audio[8:12]) != "WAVE" {
		t.Fatal("the transcriber gets the speech as WAV")
	}
}

func TestCaptionsOfSegmentsWithoutWordTimingStillFollowSentences(t *testing.T) {
	requireFFmpeg(t)
	transcriber := &fakeTranscriber{segments: []stt.Segment{{Start: 0, End: 4, Text: "Esse é o nosso novo modelo. Ele chegou ontem na loja."}}}
	client, sources := talkSource(t)
	result, err := NewCaptionsGenerator(client, transcriber, "pt").Generate(context.Background(), mediagen.Request{}, sources)
	if err != nil {
		t.Fatal(err)
	}
	vtt := string(result.Bytes)
	if strings.Contains(vtt, "modelo. Ele") || strings.Contains(vtt, "modelo.\nEle") || !strings.Contains(vtt, " --> 00:00:04.000\n") {
		t.Fatalf("vtt %s", vtt)
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
	budget  time.Duration
	bounded bool
}

func (d *deadlineTranscriber) TranscribeSegments(ctx context.Context, audio []byte, language string) (*stt.Transcription, error) {
	var deadline time.Time
	deadline, d.bounded = ctx.Deadline()
	d.budget = time.Until(deadline)
	return d.fakeTranscriber.TranscribeSegments(ctx, audio, language)
}

func TestCaptionsGiveTheTranscriptionFiveMinutes(t *testing.T) {
	requireFFmpeg(t)
	transcriber := &deadlineTranscriber{fakeTranscriber: fakeTranscriber{segments: []stt.Segment{{Start: 0, End: 1, Text: "Olá"}}}}
	client, sources := talkSource(t)
	if _, err := NewCaptionsGenerator(client, transcriber, "pt").Generate(context.Background(), mediagen.Request{}, sources); err != nil {
		t.Fatal(err)
	}
	budget := transcriber.budget
	if !transcriber.bounded || budget < 4*time.Minute+50*time.Second || budget > 5*time.Minute {
		t.Fatalf("the transcription must get a five minute budget, got %v (bounded %v)", budget, transcriber.bounded)
	}
}
