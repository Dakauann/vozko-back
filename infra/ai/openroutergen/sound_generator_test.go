package openroutergen

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vozko/domain/mediagen"
)

type recordingSoundEncoder struct {
	raw    []byte
	length time.Duration
	err    error
}

func (e *recordingSoundEncoder) EncodeSound(_ context.Context, raw []byte, length time.Duration) ([]byte, error) {
	e.raw, e.length = raw, length
	if e.err != nil {
		return nil, e.err
	}
	return []byte("m4a"), nil
}

type speechServer struct {
	path       string
	auth       string
	body       map[string]any
	status     int
	audio      []byte
	generation string
}

func (s *speechServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.path, s.auth = r.URL.Path, r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &s.body)
		if s.generation != "" {
			w.Header().Set("X-Generation-Id", s.generation)
		}
		if s.status != 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(s.status)
			_, _ = w.Write([]byte(`{"error":{"message":"prompt rejected"}}`))
			return
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write(s.audio)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func soundGenerator(t *testing.T, srv *httptest.Server, enc *recordingSoundEncoder) *SoundGenerator {
	t.Helper()
	g, err := NewSoundGenerator(Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()}, enc)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func soundRequest() mediagen.Request {
	return mediagen.Request{Kind: mediagen.KindSoundEffect, WorkspaceID: "ws", Model: "bytedance-seed/seed-audio-1-0", Prompt: " whoosh rápido de transição ", DurationMS: 1_500}
}

func TestASoundEffectIsAskedAsAPromptWithItsLengthAndLeftForTheCostLookup(t *testing.T) {
	s := &speechServer{audio: []byte("ID3mp3"), generation: "gen-sound-1"}
	enc := &recordingSoundEncoder{}
	out, err := soundGenerator(t, s.start(t), enc).Generate(context.Background(), soundRequest(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.path != "/audio/speech" || s.auth != "Bearer k" {
		t.Fatalf("path %q auth %q", s.path, s.auth)
	}
	input, _ := s.body["input"].(string)
	if s.body["model"] != "bytedance-seed/seed-audio-1-0" || s.body["response_format"] != "mp3" || !strings.HasPrefix(input, "whoosh rápido de transição") || !strings.Contains(input, "1.5 seconds") {
		t.Fatalf("body %v", s.body)
	}
	if _, voiced := s.body["voice"]; voiced {
		t.Fatal("a voice turns the prompt into speech")
	}
	if !bytes.Equal(enc.raw, []byte("ID3mp3")) || enc.length != 1500*time.Millisecond {
		t.Fatalf("encoder got %q for %v", enc.raw, enc.length)
	}
	if string(out.Bytes) != "m4a" || out.MIMEType != "audio/mp4" || out.Model != "bytedance-seed/seed-audio-1-0" || out.GenerationID != "gen-sound-1" {
		t.Fatalf("output %+v", out)
	}
	if out.CostReported || out.Billable(mediagen.KindSoundEffect) == nil {
		t.Fatal("the response carries no cost, so it must be looked up by the generation id, never assumed")
	}
}

func TestARefusedSoundEffectIsAFailureWithNothingCharged(t *testing.T) {
	s := &speechServer{status: http.StatusBadRequest}
	_, err := soundGenerator(t, s.start(t), &recordingSoundEncoder{}).Generate(context.Background(), soundRequest(), nil)
	var charged *mediagen.ChargedFailure
	if !errors.Is(err, mediagen.ErrGenerationFailed) || errors.As(err, &charged) || !strings.Contains(err.Error(), "prompt rejected") {
		t.Fatalf("got %v", err)
	}
}

func TestASoundEffectThatFailsAfterTheProviderStartedIsCharged(t *testing.T) {
	cases := map[string]struct {
		audio []byte
		enc   *recordingSoundEncoder
	}{
		"no audio":    {nil, &recordingSoundEncoder{}},
		"unencodable": {[]byte("ID3"), &recordingSoundEncoder{err: errors.New("ffmpeg")}},
	}
	for name, c := range cases {
		s := &speechServer{audio: c.audio, generation: "gen-sound-2"}
		_, err := soundGenerator(t, s.start(t), c.enc).Generate(context.Background(), soundRequest(), nil)
		var charged *mediagen.ChargedFailure
		if !errors.As(err, &charged) || charged.GenerationID != "gen-sound-2" || !errors.Is(err, mediagen.ErrGenerationFailed) {
			t.Errorf("%s: got %v", name, err)
		}
	}
}

func TestTheSoundGeneratorOnlyMakesValidSoundEffects(t *testing.T) {
	s := &speechServer{audio: []byte("ID3")}
	g := soundGenerator(t, s.start(t), &recordingSoundEncoder{})
	music := soundRequest()
	music.Kind = mediagen.KindMusic
	if _, err := g.Generate(context.Background(), music, nil); err == nil {
		t.Fatal("music went through the sound effect path")
	}
	tooLong := soundRequest()
	tooLong.DurationMS = mediagen.MaxSoundEffectMS + 1
	if _, err := g.Generate(context.Background(), tooLong, nil); !errors.Is(err, mediagen.ErrInvalidRequest) {
		t.Fatalf("got %v", err)
	}
	if s.path != "" {
		t.Fatal("an invalid request reached the provider")
	}
	if _, err := NewSoundGenerator(Config{APIKey: "k"}, nil); err == nil {
		t.Fatal("a sound generator needs an encoder")
	}
}
