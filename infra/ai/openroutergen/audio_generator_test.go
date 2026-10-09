package openroutergen

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vozko/domain/mediagen"
)

type recordingEncoder struct {
	raw []byte
	err error
}

func (e *recordingEncoder) Encode(_ context.Context, raw []byte) ([]byte, error) {
	e.raw = raw
	if e.err != nil {
		return nil, e.err
	}
	return []byte("m4a"), nil
}

type sseServer struct {
	body   map[string]any
	events []string
	status int
}

func (s *sseServer) start(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &s.body)
		if s.status != 0 {
			w.WriteHeader(s.status)
			_, _ = w.Write([]byte(`{"error":{"message":"bad request"}}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range s.events {
			fmt.Fprintf(w, "data: %s\n\n", e)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func audioEvent(data []byte) string {
	return fmt.Sprintf(`{"id":"gen-1","model":"google/lyria-3-clip-preview-20260330","choices":[{"delta":{"role":"assistant","audio":{"data":%q}}}]}`,
		base64.StdEncoding.EncodeToString(data))
}

const usageEvent = `{"id":"gen-1","model":"google/lyria-3-clip-preview-20260330","choices":[],"usage":{"prompt_tokens":16,"completion_tokens":4,"cost":0.04}}`

func audioGenerator(t *testing.T, srv *httptest.Server, enc *recordingEncoder) *AudioGenerator {
	t.Helper()
	g, err := NewAudioGenerator(Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client()}, enc)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func musicRequest() mediagen.Request {
	return mediagen.Request{Kind: mediagen.KindMusic, WorkspaceID: "ws", Model: "google/lyria-3-clip-preview", Prompt: "jingle acústico"}
}

func TestMusicIsStreamedJoinedEncodedAndPriced(t *testing.T) {
	s := &sseServer{events: []string{audioEvent([]byte("ID3a")), audioEvent([]byte("bc")), usageEvent, "[DONE]"}}
	enc := &recordingEncoder{}
	out, err := audioGenerator(t, s.start(t), enc).Generate(context.Background(), musicRequest(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(enc.raw) != "ID3abc" || string(out.Bytes) != "m4a" || out.MIMEType != "audio/mp4" {
		t.Fatalf("raw %q out %+v", enc.raw, out)
	}
	if !out.CostReported || out.ProviderCostMicros != 40_000 || out.GenerationID != "gen-1" || out.Model != "google/lyria-3-clip-preview-20260330" {
		t.Fatalf("output %+v", out)
	}
	if s.body["stream"] != true || s.body["audio"] != nil {
		t.Fatalf("music request %+v", s.body)
	}
}

func spokenEvent(data []byte, transcript string) string {
	return fmt.Sprintf(`{"id":"gen-1","model":"openai/gpt-audio-mini","choices":[{"delta":{"audio":{"data":%q,"transcript":%q}}}]}`,
		base64.StdEncoding.EncodeToString(data), transcript)
}

func voiceRequest() mediagen.Request {
	return mediagen.Request{Kind: mediagen.KindVoice, WorkspaceID: "ws", Model: "openai/gpt-audio-mini", Prompt: "Conheça a Vozko, o CRM da sua empresa.", Voice: "verse"}
}

func TestAVoiceOverAsksForTheVoiceAndWrapsThePCMAsWav(t *testing.T) {
	s := &sseServer{events: []string{spokenEvent([]byte{1, 0}, "Conheça a Vozko, "), spokenEvent([]byte{2, 0}, "o CRM da sua empresa."), usageEvent, "[DONE]"}}
	enc := &recordingEncoder{}
	if _, err := audioGenerator(t, s.start(t), enc).Generate(context.Background(), voiceRequest(), nil); err != nil {
		t.Fatal(err)
	}
	audio, _ := s.body["audio"].(map[string]any)
	if audio["voice"] != "verse" || audio["format"] != "pcm16" {
		t.Fatalf("voice request %+v", s.body)
	}
	messages, _ := s.body["messages"].([]any)
	if len(messages) != 2 || !strings.HasPrefix(string(enc.raw), "RIFF") || len(enc.raw) != 44+4 {
		t.Fatalf("messages %+v raw %d bytes", messages, len(enc.raw))
	}
}

func TestAVoiceOverAsksTheModelToReadTheScriptAndNothingElse(t *testing.T) {
	s := &sseServer{events: []string{spokenEvent([]byte{1, 0}, "Conheça a Vozko, o CRM da sua empresa."), usageEvent, "[DONE]"}}
	if _, err := audioGenerator(t, s.start(t), &recordingEncoder{}).Generate(context.Background(), voiceRequest(), nil); err != nil {
		t.Fatal(err)
	}
	messages, _ := s.body["messages"].([]any)
	user, _ := messages[1].(map[string]any)
	if s.body["temperature"] != 0.0 || user["content"] != "<script>Conheça a Vozko, o CRM da sua empresa.</script>" {
		t.Fatalf("voice request %+v", s.body)
	}
}

func TestAVoiceThatSaysSomethingElseIsAChargedFailure(t *testing.T) {
	said := "Claro! Conheça a Vozko, o CRM da sua empresa, a melhor do mercado."
	s := &sseServer{events: []string{spokenEvent([]byte{1, 0}, said), usageEvent, "[DONE]"}}
	enc := &recordingEncoder{}
	_, err := audioGenerator(t, s.start(t), enc).Generate(context.Background(), voiceRequest(), nil)
	var charged *mediagen.ChargedFailure
	if !errors.As(err, &charged) || charged.GenerationID != "gen-1" || !strings.Contains(err.Error(), said) {
		t.Fatalf("got %v", err)
	}
	if enc.raw != nil {
		t.Fatal("a voice that strayed from the script must never be delivered")
	}
}

func TestMusicHasNoScriptToCheck(t *testing.T) {
	s := &sseServer{events: []string{audioEvent([]byte("ID3")), usageEvent, "[DONE]"}}
	if _, err := audioGenerator(t, s.start(t), &recordingEncoder{}).Generate(context.Background(), musicRequest(), nil); err != nil {
		t.Fatal(err)
	}
	if _, set := s.body["temperature"]; set {
		t.Fatalf("music request %+v", s.body)
	}
}

func TestAMissingCostIsReportedAsMissingWithItsGeneration(t *testing.T) {
	s := &sseServer{events: []string{audioEvent([]byte("ID3")), "[DONE]"}}
	out, err := audioGenerator(t, s.start(t), &recordingEncoder{}).Generate(context.Background(), musicRequest(), nil)
	if err != nil || out.CostReported || out.GenerationID != "gen-1" {
		t.Fatalf("output %+v err %v", out, err)
	}
}

func TestAStreamThatStopsEarlyIsAChargedFailure(t *testing.T) {
	for name, events := range map[string][]string{
		"cut":           {audioEvent([]byte("ID3"))},
		"provider said": {audioEvent([]byte("ID3")), `{"id":"gen-1","error":{"message":"overloaded"}}`},
		"no audio":      {usageEvent, "[DONE]"},
	} {
		s := &sseServer{events: events}
		_, err := audioGenerator(t, s.start(t), &recordingEncoder{}).Generate(context.Background(), musicRequest(), nil)
		var charged *mediagen.ChargedFailure
		if !errors.As(err, &charged) || charged.GenerationID != "gen-1" {
			t.Errorf("%s: got %v", name, err)
		}
	}
}

func TestAnEncodingFailureAfterTheGenerationIsCharged(t *testing.T) {
	s := &sseServer{events: []string{audioEvent([]byte("ID3")), usageEvent, "[DONE]"}}
	_, err := audioGenerator(t, s.start(t), &recordingEncoder{err: errors.New("ffmpeg")}).Generate(context.Background(), musicRequest(), nil)
	var charged *mediagen.ChargedFailure
	if !errors.As(err, &charged) {
		t.Fatalf("got %v", err)
	}
}

func TestARefusedRequestIsNotCharged(t *testing.T) {
	s := &sseServer{status: http.StatusBadRequest}
	_, err := audioGenerator(t, s.start(t), &recordingEncoder{}).Generate(context.Background(), musicRequest(), nil)
	var charged *mediagen.ChargedFailure
	if err == nil || errors.As(err, &charged) || !errors.Is(err, mediagen.ErrGenerationFailed) {
		t.Fatalf("got %v", err)
	}
}
