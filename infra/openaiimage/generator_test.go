package openaiimage

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"vozko/domain/advertising"
)

func pngBytes(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 90, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type stub struct {
	body    map[string]any
	auth    string
	path    string
	status  int
	respond string
}

func generatorWith(t *testing.T, s *stub) *Generator {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.path = r.URL.Path
		s.auth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &s.body)
		status := s.status
		if status == 0 {
			status = http.StatusOK
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(s.respond))
	}))
	t.Cleanup(srv.Close)
	g, err := New(Config{APIKey: "key", BaseURL: srv.URL + "/api/v1/"})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func request(aspect advertising.Aspect) advertising.ImageRequest {
	return advertising.ImageRequest{WorkspaceID: "ws", Prompt: "Pizza artesanal na mesa", Aspect: aspect}
}

func TestGenerateNormalisesToTheExactAspectSize(t *testing.T) {
	tests := []struct {
		aspect advertising.Aspect
		ratio  string
		width  int
		height int
	}{
		{aspect: advertising.AspectSquare, ratio: "1:1", width: 1080, height: 1080},
		{aspect: advertising.AspectPortrait, ratio: "3:4", width: 1080, height: 1350},
		{aspect: advertising.AspectStory, ratio: "9:16", width: 1080, height: 1920},
	}
	encoded := base64.StdEncoding.EncodeToString(pngBytes(t, 300, 400))
	for _, tt := range tests {
		t.Run(string(tt.aspect), func(t *testing.T) {
			s := &stub{respond: `{"created":1,"data":[{"b64_json":"` + encoded + `","media_type":"image/png"}],"usage":{"cost":0.04}}`}
			g := generatorWith(t, s)

			out, err := g.Generate(context.Background(), request(tt.aspect))
			if err != nil {
				t.Fatal(err)
			}
			if s.path != "/api/v1/images" || s.auth != "Bearer key" {
				t.Fatalf("path %s auth %s", s.path, s.auth)
			}
			if s.body["model"] != DefaultModel || s.body["aspect_ratio"] != tt.ratio || !strings.HasPrefix(s.body["prompt"].(string), "Pizza artesanal na mesa") {
				t.Fatalf("body = %v", s.body)
			}
			if out.MIMEType != "image/jpeg" || out.Model != DefaultModel || out.ProviderCostMicros != 40000 {
				t.Fatalf("out = %+v", out)
			}
			img, err := jpeg.Decode(bytes.NewReader(out.Bytes))
			if err != nil {
				t.Fatal(err)
			}
			if b := img.Bounds(); b.Dx() != tt.width || b.Dy() != tt.height {
				t.Fatalf("size = %dx%d", b.Dx(), b.Dy())
			}
		})
	}
}

func TestGenerateAcceptsDataURLAndReportedModel(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString(pngBytes(t, 64, 64))
	s := &stub{respond: `{"model":"openai/gpt-image-2.5-flare-20260901","data":[{"url":"data:image/png;base64,` + encoded + `"}],"usage":{"cost":0.0000011}}`}
	out, err := generatorWith(t, s).Generate(context.Background(), request(advertising.AspectSquare))
	if err != nil {
		t.Fatal(err)
	}
	if out.Model != "openai/gpt-image-2.5-flare-20260901" || out.ProviderCostMicros != 2 {
		t.Fatalf("out = %+v", out)
	}
}

func TestGenerateDownloadsHTTPSImages(t *testing.T) {
	image := pngBytes(t, 50, 80)
	imageSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(image)
	}))
	t.Cleanup(imageSrv.Close)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"url":"` + imageSrv.URL + `/img.png"}],"usage":{"cost":0.01}}`))
	}))
	t.Cleanup(api.Close)
	g, err := New(Config{APIKey: "key", BaseURL: api.URL, HTTPClient: imageSrv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	out, err := g.Generate(context.Background(), request(advertising.AspectStory))
	if err != nil {
		t.Fatal(err)
	}
	if out.ProviderCostMicros != 10000 || len(out.Bytes) == 0 {
		t.Fatalf("out = %+v", out)
	}
}

func TestGenerateFailures(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString(pngBytes(t, 10, 10))
	tests := []struct {
		name      string
		status    int
		respond   string
		sentinel  bool
		substring string
	}{
		{name: "missing image", respond: `{"data":[],"usage":{"cost":0.01}}`, sentinel: true},
		{name: "empty item", respond: `{"data":[{"media_type":"image/png"}],"usage":{"cost":0.01}}`, sentinel: true},
		{name: "non 2xx", status: http.StatusPaymentRequired, respond: `{"error":{"message":"Insufficient credits"}}`, sentinel: true, substring: "402"},
		{name: "unreadable image", respond: `{"data":[{"b64_json":"aGVsbG8="}],"usage":{"cost":0.01}}`, sentinel: true},
		{name: "negative cost", respond: `{"data":[{"b64_json":"` + encoded + `"}],"usage":{"cost":-1}}`, substring: "cost"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &stub{status: tt.status, respond: tt.respond}
			_, err := generatorWith(t, s).Generate(context.Background(), request(advertising.AspectSquare))
			if err == nil {
				t.Fatal("expected an error")
			}
			if tt.sentinel && !errors.Is(err, advertising.ErrImageGenerationFailed) {
				t.Fatalf("err = %v", err)
			}
			if tt.substring != "" && !strings.Contains(err.Error(), tt.substring) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestGenerateValidatesBeforeCalling(t *testing.T) {
	s := &stub{respond: `{}`}
	g := generatorWith(t, s)
	if _, err := g.Generate(context.Background(), advertising.ImageRequest{WorkspaceID: "ws", Prompt: " ", Aspect: advertising.AspectSquare}); err == nil || s.path != "" {
		t.Fatalf("err %v path %q", err, s.path)
	}
}

func TestNewRequiresAnAPIKey(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("expected an error")
	}
}

func TestUnreportedCostComesBackAsUnknownSoThePaidImageIsKept(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString(pngBytes(t, 10, 10))
	s := &stub{respond: `{"data":[{"b64_json":"` + encoded + `"}]}`}
	out, err := generatorWith(t, s).Generate(context.Background(), request(advertising.AspectSquare))
	if err != nil || out.ProviderCostMicros != 0 || len(out.Bytes) == 0 {
		t.Fatalf("out %+v err %v", out, err)
	}
}
