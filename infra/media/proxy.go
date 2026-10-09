package media_infra

import (
	"context"
	"net/http"
	"path/filepath"

	"vozko/domain/mediagen"
)

const (
	proxyScale          = "scale='if(gt(iw,ih),-2,trunc(min(720,iw)/2)*2)':'if(gt(iw,ih),trunc(min(720,ih)/2)*2,-2)'"
	proxyFrameRate      = "30"
	proxyKeyframeFrames = "15"
	proxyMaxRate        = "1500k"
	proxyBufferSize     = "3000k"
)

type ProxyGenerator struct {
	client *http.Client
}

var _ mediagen.Generator = (*ProxyGenerator)(nil)

func NewProxyGenerator(client *http.Client) *ProxyGenerator {
	return &ProxyGenerator{client: client}
}

func (g *ProxyGenerator) Generate(ctx context.Context, _ mediagen.Request, sources []mediagen.Source) (*mediagen.Output, error) {
	raw, err := soleSource(ctx, g.client, sources, maxProcessingSourceBytes)
	if err != nil {
		return nil, err
	}
	proxy, err := encodeProxy(ctx, raw)
	if err != nil {
		return nil, err
	}
	return &mediagen.Output{Bytes: proxy, MIMEType: "video/mp4", Model: processingModel}, nil
}

func encodeProxy(ctx context.Context, raw []byte) ([]byte, error) {
	dir, cleanup, err := workDir()
	if err != nil {
		return nil, err
	}
	defer cleanup()
	in, err := writeInput(dir, "source", raw)
	if err != nil {
		return nil, err
	}
	sound, err := hasAudioStream(ctx, in)
	if err != nil {
		return nil, err
	}
	out := filepath.Join(dir, "proxy.mp4")
	if err := runFFmpegArgs(ctx, proxyArgs(in, out, sound)...); err != nil {
		return nil, err
	}
	return readOutput(out)
}

func proxyArgs(in, out string, sound bool) []string {
	args := []string{
		"-i", in, "-map", "0:v:0", "-vf", proxyScale, "-r", proxyFrameRate,
		"-c:v", "libx264", "-preset", "veryfast", "-crf", "28", "-maxrate", proxyMaxRate, "-bufsize", proxyBufferSize, "-pix_fmt", "yuv420p",
		"-g", proxyKeyframeFrames, "-keyint_min", proxyKeyframeFrames, "-sc_threshold", "0",
	}
	if sound {
		args = append(args, "-map", "0:a:0", "-c:a", "aac", "-b:a", "96k", "-ac", "2")
	} else {
		args = append(args, "-an")
	}
	return append(args, "-movflags", "+faststart", "-y", out)
}
