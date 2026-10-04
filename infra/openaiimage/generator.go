package openaiimage

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/disintegration/imaging"
	_ "golang.org/x/image/webp"

	"vozko/domain/imagegen"
)

const (
	DefaultBaseURL  = "https://openrouter.ai/api/v1"
	defaultTimeout  = 120 * time.Second
	maxImageBytes   = 20 << 20
	maxErrorBody    = 512
	jpegQuality     = 90
	outputMIMEType  = "image/jpeg"
	maxResponseSize = 64 << 20
)

var _ imagegen.Generator = (*Generator)(nil)

var aspectRatios = map[imagegen.Aspect]string{
	imagegen.AspectSquare:   "1:1",
	imagegen.AspectPortrait: "3:4",
	imagegen.AspectStory:    "9:16",
}

type Config struct {
	APIKey     string
	BaseURL    string
	HTTPClient *http.Client
}

type Generator struct {
	apiKey  string
	baseURL string
	http    *http.Client
}

func New(cfg Config) (*Generator, error) {
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return nil, fmt.Errorf("openaiimage: api key is required")
	}
	baseURL := strings.TrimSuffix(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: defaultTimeout}
	}
	return &Generator{apiKey: apiKey, baseURL: baseURL, http: client}, nil
}

type generationRequest struct {
	Model           string           `json:"model"`
	Prompt          string           `json:"prompt"`
	AspectRatio     string           `json:"aspect_ratio"`
	OutputFormat    string           `json:"output_format"`
	N               int              `json:"n"`
	InputReferences []inputReference `json:"input_references,omitempty"`
}

type inputReference struct {
	Type     string        `json:"type"`
	ImageURL referenceLink `json:"image_url"`
}

type referenceLink struct {
	URL string `json:"url"`
}

type generatedItem struct {
	B64JSON   string `json:"b64_json"`
	URL       string `json:"url"`
	MediaType string `json:"media_type"`
}

type generationResponse struct {
	Model string          `json:"model"`
	Data  []generatedItem `json:"data"`
	Usage *struct {
		Cost *float64 `json:"cost"`
	} `json:"usage"`
}

func (g *Generator) Generate(ctx context.Context, req imagegen.Request, references []imagegen.ReferenceImage) (*imagegen.GeneratedImage, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	inputs, err := inputReferences(req.ReferenceMediaIDs, references)
	if err != nil {
		return nil, err
	}
	size, err := req.Aspect.Size()
	if err != nil {
		return nil, err
	}
	ratio, ok := aspectRatios[req.Aspect]
	if !ok {
		return nil, fmt.Errorf("openaiimage: no aspect ratio for %q", req.Aspect)
	}
	requested := strings.TrimSpace(req.Model)
	out, err := g.request(ctx, generationRequest{
		Model:           requested,
		Prompt:          promptFor(req.Prompt, ratio),
		AspectRatio:     ratio,
		OutputFormat:    "png",
		N:               1,
		InputReferences: inputs,
	})
	if err != nil {
		return nil, err
	}
	if len(out.Data) == 0 {
		return nil, fmt.Errorf("%w: the provider returned no image", imagegen.ErrGenerationFailed)
	}
	raw, err := g.imageBytes(ctx, out.Data[0])
	if err != nil {
		return nil, err
	}
	normalized, err := normalize(raw, size)
	if err != nil {
		return nil, err
	}
	cost, err := costMicros(out)
	if err != nil {
		return nil, err
	}
	model := out.Model
	if model == "" {
		model = requested
	}
	return &imagegen.GeneratedImage{Bytes: normalized, MIMEType: outputMIMEType, Model: model, ProviderCostMicros: cost}, nil
}

func inputReferences(ids []string, references []imagegen.ReferenceImage) ([]inputReference, error) {
	if len(references) != len(ids) {
		return nil, fmt.Errorf("openaiimage: %d reference images resolved for %d requested", len(references), len(ids))
	}
	inputs := make([]inputReference, 0, len(references))
	for i, ref := range references {
		if ref.MediaID != strings.TrimSpace(ids[i]) {
			return nil, fmt.Errorf("openaiimage: reference %d is %q, want %q", i, ref.MediaID, ids[i])
		}
		if !strings.HasPrefix(ref.URL, "https://") {
			return nil, fmt.Errorf("openaiimage: reference %s is not served over https", ref.MediaID)
		}
		inputs = append(inputs, inputReference{Type: "image_url", ImageURL: referenceLink{URL: ref.URL}})
	}
	return inputs, nil
}

func promptFor(prompt, ratio string) string {
	return strings.TrimSpace(prompt) + "\n\nCreate a high quality image in " + ratio +
		" aspect ratio. Do not add any text, captions, logos or watermarks unless the request above asks for them."
}

func (g *Generator) request(ctx context.Context, body generationRequest) (*generationResponse, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+"/images", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+g.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := g.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openaiimage: request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return nil, fmt.Errorf("openaiimage: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: status %d: %s", imagegen.ErrGenerationFailed, resp.StatusCode, truncate(raw))
	}
	var out generationResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("openaiimage: decode response: %w (body: %s)", err, truncate(raw))
	}
	return &out, nil
}

func (g *Generator) imageBytes(ctx context.Context, item generatedItem) ([]byte, error) {
	switch {
	case item.B64JSON != "":
		return decodeBase64(item.B64JSON)
	case strings.HasPrefix(item.URL, "data:"):
		_, encoded, found := strings.Cut(item.URL, ";base64,")
		if !found {
			return nil, fmt.Errorf("%w: image data url is not base64", imagegen.ErrGenerationFailed)
		}
		return decodeBase64(encoded)
	case strings.HasPrefix(item.URL, "https://"):
		return g.download(ctx, item.URL)
	}
	return nil, fmt.Errorf("%w: the provider returned no image", imagegen.ErrGenerationFailed)
}

func decodeBase64(encoded string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return nil, fmt.Errorf("%w: invalid base64 image: %w", imagegen.ErrGenerationFailed, err)
	}
	if len(raw) > maxImageBytes {
		return nil, fmt.Errorf("%w: image exceeds %d bytes", imagegen.ErrGenerationFailed, maxImageBytes)
	}
	return raw, nil
}

func (g *Generator) download(ctx context.Context, rawURL string) ([]byte, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := g.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openaiimage: download image: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%w: image download status %d", imagegen.ErrGenerationFailed, resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes+1))
	if err != nil {
		return nil, fmt.Errorf("openaiimage: download image: %w", err)
	}
	if len(raw) > maxImageBytes {
		return nil, fmt.Errorf("%w: image exceeds %d bytes", imagegen.ErrGenerationFailed, maxImageBytes)
	}
	return raw, nil
}

func normalize(raw []byte, size imagegen.Size) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("%w: unreadable image: %w", imagegen.ErrGenerationFailed, err)
	}
	cropped := imaging.Fill(img, size.Width, size.Height, imaging.Center, imaging.Lanczos)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, cropped, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return nil, fmt.Errorf("openaiimage: encode jpeg: %w", err)
	}
	return buf.Bytes(), nil
}

func costMicros(out *generationResponse) (int64, error) {
	if out.Usage == nil || out.Usage.Cost == nil {
		return 0, nil
	}
	cost := *out.Usage.Cost
	if cost < 0 || math.IsNaN(cost) || math.IsInf(cost, 0) {
		return 0, fmt.Errorf("openaiimage: invalid generation cost %v", cost)
	}
	return int64(math.Ceil(cost*1e6 - 1e-9)), nil
}

func truncate(raw []byte) string {
	if len(raw) <= maxErrorBody {
		return string(raw)
	}
	return string(raw[:maxErrorBody]) + "..."
}
