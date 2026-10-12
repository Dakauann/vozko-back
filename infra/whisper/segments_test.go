package whisper

import (
	"context"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"vozko/domain/stt"
)

var _ stt.SegmentTranscriber = (*Client)(nil)

func whisperCppServer(t *testing.T, formats *[]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		reader := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := reader.NextPart()
			if err != nil {
				break
			}
			if part.FormName() == "response_format" {
				value, _ := io.ReadAll(part)
				*formats = append(*formats, string(value))
			}
		}
		if (*formats)[len(*formats)-1] == "verbose_json" {
			_, _ = w.Write([]byte(`{"text":" Olá mundo. Tudo bem?","language":"pt","duration":3.2,"segments":[{"id":0,"start":0.0,"end":1.4,"text":" Olá mundo."},{"id":1,"start":1.5,"end":3.1,"text":" Tudo bem?"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"text":" Olá mundo. Tudo bem?"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSegmentsAreAskedFromWhisperCppAsVerboseJSON(t *testing.T) {
	var formats []string
	srv := whisperCppServer(t, &formats)
	client := NewClient(Config{BaseURL: srv.URL, ServerType: ServerTypeWhisperCpp, Language: "pt"})
	result, err := client.TranscribeSegments(context.Background(), []byte("RIFF0000WAVEdata"), "pt")
	if err != nil {
		t.Fatal(err)
	}
	if formats[0] != "verbose_json" || len(result.Segments) != 2 || result.Segments[1].Start != 1.5 || result.Segments[1].Text != "Tudo bem?" {
		t.Fatalf("formats %v result %+v", formats, result)
	}
	if _, err := client.Transcribe(context.Background(), []byte("RIFF0000WAVEdata"), "pt"); err != nil || formats[1] != "json" {
		t.Fatalf("plain transcription changed: formats %v err %v", formats, err)
	}
}

func formServer(t *testing.T, fields map[string]string, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		reader := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := reader.NextPart()
			if err != nil {
				break
			}
			value, _ := io.ReadAll(part)
			fields[part.FormName()] = string(value)
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

const tokenTimedSegments = `{"text":" Olá, você já conhece a promoção?","segments":[{"id":0,"start":0.0,"end":2.6,"text":" Olá, você já conhece a promoção?","words":[` +
	`{"word":" Ol","start":0.0,"end":0.2},{"word":"á","start":0.2,"end":0.4},{"word":",","start":0.4,"end":0.45},` +
	`{"word":" ","start":0.85,"end":0.9},{"word":"você","start":0.9,"end":1.2},{"word":" já","start":1.2,"end":1.35},{"word":" conhece","start":1.35,"end":1.8},` +
	`{"word":" a","start":1.8,"end":1.9},{"word":" promo","start":1.9,"end":2.2},{"word":"ção","start":2.2,"end":2.45},{"word":"?","start":2.45,"end":2.5}]}]}`

func TestSegmentsCarryWordsRebuiltFromTimedTokens(t *testing.T) {
	fields := map[string]string{}
	srv := formServer(t, fields, tokenTimedSegments)
	client := NewClient(Config{BaseURL: srv.URL, ServerType: ServerTypeWhisperCpp, Language: "pt"})
	result, err := client.TranscribeSegments(context.Background(), []byte("RIFF0000WAVEdata"), "pt")
	if err != nil {
		t.Fatal(err)
	}
	if fields["token_timestamps"] != "true" || fields["split_on_word"] != "true" {
		t.Fatalf("word timing was not asked for: %v", fields)
	}
	words := result.Segments[0].Words
	want := []stt.Word{
		{Text: "Olá,", Start: 0, End: 0.45},
		{Text: "você", Start: 0.9, End: 1.2},
		{Text: "já", Start: 1.2, End: 1.35},
		{Text: "conhece", Start: 1.35, End: 1.8},
		{Text: "a", Start: 1.8, End: 1.9},
		{Text: "promoção?", Start: 1.9, End: 2.5},
	}
	if len(words) != len(want) {
		t.Fatalf("words %+v", words)
	}
	for i := range want {
		if words[i] != want[i] {
			t.Fatalf("word %d is %+v, want %+v", i, words[i], want[i])
		}
	}
}

func TestASegmentWithUntimedTokensHasNoWords(t *testing.T) {
	body := `{"text":" Olá","segments":[{"id":0,"start":0.0,"end":1.0,"text":" Olá","words":[{"word":" Ol"},{"word":"á","start":0.2,"end":0.4}]},` +
		`{"id":1,"start":1.0,"end":2.0,"text":" mundo","words":[{"word":" mundo","start":-0.01,"end":-0.01}]}]}`
	srv := formServer(t, map[string]string{}, body)
	client := NewClient(Config{BaseURL: srv.URL, ServerType: ServerTypeWhisperCpp, Language: "pt"})
	result, err := client.TranscribeSegments(context.Background(), []byte("RIFF0000WAVEdata"), "pt")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Segments) != 2 || result.Segments[0].Words != nil || result.Segments[1].Words != nil {
		t.Fatalf("segments %+v", result.Segments)
	}
}

func TestPlainTranscriptionDoesNotAskForWordTiming(t *testing.T) {
	fields := map[string]string{}
	srv := formServer(t, fields, `{"text":" Olá mundo"}`)
	client := NewClient(Config{BaseURL: srv.URL, ServerType: ServerTypeWhisperCpp, Language: "pt"})
	if _, err := client.Transcribe(context.Background(), []byte("RIFF0000WAVEdata"), "pt"); err != nil {
		t.Fatal(err)
	}
	if _, asked := fields["token_timestamps"]; asked {
		t.Fatalf("fields %v", fields)
	}
	if _, asked := fields["split_on_word"]; asked {
		t.Fatalf("fields %v", fields)
	}
}
