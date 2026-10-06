package openroutergen

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

	"vozko/domain/mediagen"
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

func generatorWith(t *testing.T, s *stub) *ImageGenerator {
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
	g, err := NewImageGenerator(Config{APIKey: "key", BaseURL: srv.URL + "/api/v1/"})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func request(aspect mediagen.Aspect) mediagen.Request {
	return mediagen.Request{Kind: mediagen.KindImage, WorkspaceID: "ws", Model: chosenModel, Prompt: "Pizza artesanal na mesa", Aspect: aspect}
}

func TestGenerateNormalisesToTheExactAspectSize(t *testing.T) {
	tests := []struct {
		aspect mediagen.Aspect
		ratio  string
		width  int
		height int
	}{
		{aspect: mediagen.AspectSquare, ratio: "1:1", width: 1080, height: 1080},
		{aspect: mediagen.AspectPortrait, ratio: "3:4", width: 1080, height: 1350},
		{aspect: mediagen.AspectStory, ratio: "9:16", width: 1080, height: 1920},
		{aspect: mediagen.AspectLandscape, ratio: "16:9", width: 1920, height: 1080},
	}
	encoded := base64.StdEncoding.EncodeToString(pngBytes(t, 300, 400))
	for _, tt := range tests {
		t.Run(string(tt.aspect), func(t *testing.T) {
			s := &stub{respond: `{"created":1,"data":[{"b64_json":"` + encoded + `","media_type":"image/png"}],"usage":{"cost":0.04}}`}
			g := generatorWith(t, s)

			out, err := g.Generate(context.Background(), request(tt.aspect), nil)
			if err != nil {
				t.Fatal(err)
			}
			if s.path != "/api/v1/images" || s.auth != "Bearer key" {
				t.Fatalf("path %s auth %s", s.path, s.auth)
			}
			if s.body["model"] != chosenModel || s.body["aspect_ratio"] != tt.ratio || !strings.HasPrefix(s.body["prompt"].(string), "Pizza artesanal na mesa") {
				t.Fatalf("body = %v", s.body)
			}
			if out.MIMEType != "image/jpeg" || out.Model != chosenModel || out.ProviderCostMicros != 40000 {
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
	out, err := generatorWith(t, s).Generate(context.Background(), request(mediagen.AspectSquare), nil)
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
	g, err := NewImageGenerator(Config{APIKey: "key", BaseURL: api.URL, HTTPClient: imageSrv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	out, err := g.Generate(context.Background(), request(mediagen.AspectStory), nil)
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
			_, err := generatorWith(t, s).Generate(context.Background(), request(mediagen.AspectSquare), nil)
			if err == nil {
				t.Fatal("expected an error")
			}
			if tt.sentinel && !errors.Is(err, mediagen.ErrGenerationFailed) {
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
	if _, err := g.Generate(context.Background(), mediagen.Request{Kind: mediagen.KindImage, WorkspaceID: "ws", Prompt: " ", Aspect: mediagen.AspectSquare}, nil); err == nil || s.path != "" {
		t.Fatalf("err %v path %q", err, s.path)
	}
}

func TestNewRequiresAnAPIKey(t *testing.T) {
	if _, err := NewImageGenerator(Config{}); err == nil {
		t.Fatal("expected an error")
	}
}

func TestUnreportedCostComesBackAsUnknownSoThePaidImageIsKept(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString(pngBytes(t, 10, 10))
	s := &stub{respond: `{"data":[{"b64_json":"` + encoded + `"}]}`}
	out, err := generatorWith(t, s).Generate(context.Background(), request(mediagen.AspectSquare), nil)
	if err != nil || out.ProviderCostMicros != 0 || len(out.Bytes) == 0 {
		t.Fatalf("out %+v err %v", out, err)
	}
}

func referencedRequest(ids ...string) mediagen.Request {
	req := request(mediagen.AspectSquare)
	req.ReferenceMediaIDs = ids
	return req
}

func TestReferenceImagesAreSentAsInputReferencesInOrder(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString(pngBytes(t, 10, 10))
	s := &stub{respond: `{"data":[{"b64_json":"` + encoded + `"}],"usage":{"cost":0.04}}`}
	refs := []mediagen.Source{{MediaID: "m-2", URL: "https://cdn/m-2.jpg"}, {MediaID: "m-1", URL: "https://cdn/m-1.png"}}
	if _, err := generatorWith(t, s).Generate(context.Background(), referencedRequest("m-2", "m-1"), refs); err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(s.body["input_references"])
	want := `[{"image_url":{"url":"https://cdn/m-2.jpg"},"type":"image_url"},{"image_url":{"url":"https://cdn/m-1.png"},"type":"image_url"}]`
	if string(got) != want {
		t.Fatalf("input_references = %s", got)
	}
}

func TestARequestWithoutReferencesSendsNone(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString(pngBytes(t, 10, 10))
	s := &stub{respond: `{"data":[{"b64_json":"` + encoded + `"}],"usage":{"cost":0.04}}`}
	if _, err := generatorWith(t, s).Generate(context.Background(), request(mediagen.AspectSquare), nil); err != nil {
		t.Fatal(err)
	}
	if _, present := s.body["input_references"]; present {
		t.Fatalf("body = %v", s.body)
	}
}

func TestReferencesMustBeTheResolvedRequestReferencesOverHTTPS(t *testing.T) {
	cases := map[string][]mediagen.Source{
		"missing":    {{MediaID: "m-1", URL: "https://cdn/m-1.png"}},
		"reordered":  {{MediaID: "m-2", URL: "https://cdn/m-2.png"}, {MediaID: "m-1", URL: "https://cdn/m-1.png"}},
		"plain http": {{MediaID: "m-1", URL: "http://cdn/m-1.png"}, {MediaID: "m-2", URL: "https://cdn/m-2.png"}},
	}
	for name, refs := range cases {
		s := &stub{respond: `{}`}
		if _, err := generatorWith(t, s).Generate(context.Background(), referencedRequest("m-1", "m-2"), refs); err == nil || s.path != "" {
			t.Fatalf("%s: err %v path %q", name, err, s.path)
		}
	}
}

const chosenModel = "google/gemini-3-pro-image"
