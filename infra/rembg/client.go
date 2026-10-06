package rembg

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"

	"vozko/domain/mediagen"
)

const (
	Model         = "isnet-general-use"
	maxImageBytes = 30 << 20
	maxErrorBody  = 512
	pngSignature  = "\x89PNG\r\n\x1a\n"
)

type Client struct {
	baseURL string
	http    *http.Client
	media   *http.Client
}

var _ mediagen.Generator = (*Client)(nil)

func New(baseURL string, httpClient, mediaClient *http.Client) (*Client, error) {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" || httpClient == nil || mediaClient == nil {
		return nil, fmt.Errorf("rembg: a base url and http clients are required")
	}
	return &Client{baseURL: base, http: httpClient, media: mediaClient}, nil
}

func (c *Client) Generate(ctx context.Context, _ mediagen.Request, sources []mediagen.Source) (*mediagen.Output, error) {
	if len(sources) != 1 {
		return nil, fmt.Errorf("rembg: %d sources resolved where one is required", len(sources))
	}
	image, err := c.download(ctx, sources[0].URL)
	if err != nil {
		return nil, err
	}
	cut, err := c.remove(ctx, image)
	if err != nil {
		return nil, err
	}
	return &mediagen.Output{Bytes: cut, MIMEType: "image/png", Model: "rembg/" + Model}, nil
}

func (c *Client) download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	res, err := c.media.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rembg: source download status %d", res.StatusCode)
	}
	return bounded(res.Body)
}

func (c *Client) remove(ctx context.Context, image []byte) ([]byte, error) {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	if err := form.WriteField("model", Model); err != nil {
		return nil, err
	}
	part, err := form.CreateFormFile("file", "source")
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(image); err != nil {
		return nil, err
	}
	if err := form.Close(); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/remove", &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	res, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: rembg unreachable: %v", mediagen.ErrGenerationFailed, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(res.Body, maxErrorBody))
		return nil, fmt.Errorf("%w: rembg status %d: %s", mediagen.ErrGenerationFailed, res.StatusCode, raw)
	}
	out, err := bounded(res.Body)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(string(out), pngSignature) {
		return nil, fmt.Errorf("%w: rembg did not return a png", mediagen.ErrGenerationFailed)
	}
	return out, nil
}

func bounded(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxImageBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxImageBytes {
		return nil, fmt.Errorf("rembg: image larger than %d bytes", maxImageBytes)
	}
	return data, nil
}
