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
