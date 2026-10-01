package mediaprobe

import (
	"bytes"
	"encoding/base64"
	"image"
	"log"
	"runtime"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"github.com/disintegration/imaging"
	"go.n16f.net/thumbhash"
	_ "golang.org/x/image/webp"

	"vozko/domain/conversation"
)

const maxDecodedPixels = 16_000_000

type Inspector struct {
	decodes chan struct{}
}

func New() *Inspector {
	return &Inspector{decodes: make(chan struct{}, runtime.GOMAXPROCS(0))}
}

func (in *Inspector) Inspect(data []byte, mediaType conversation.MediaType) (layout conversation.MediaLayout) {
	if !inspectable(mediaType) || len(data) == 0 {
		return conversation.MediaLayout{}
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf("[mediaprobe] decoder panicked on a %s: %v", mediaType, recovered)
			layout = conversation.MediaLayout{}
		}
	}()

	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return conversation.MediaLayout{}
	}
	header := conversation.MediaLayout{Width: config.Width, Height: config.Height}
	if config.Width*config.Height > maxDecodedPixels {
		return headerUnlessRotatable(header, format)
	}

	in.decodes <- struct{}{}
	defer func() { <-in.decodes }()

	img, err := imaging.Decode(bytes.NewReader(data), imaging.AutoOrientation(true))
	if err != nil {
		return headerUnlessRotatable(header, format)
	}
	bounds := img.Bounds()
	return conversation.MediaLayout{
		Width:     bounds.Dx(),
		Height:    bounds.Dy(),
		Thumbhash: base64.StdEncoding.EncodeToString(thumbhash.EncodeImage(img)),
	}
}

func inspectable(mediaType conversation.MediaType) bool {
	return mediaType == conversation.MediaTypeImage || mediaType == conversation.MediaTypeSticker
}

func headerUnlessRotatable(header conversation.MediaLayout, format string) conversation.MediaLayout {
	if format == "jpeg" {
		return conversation.MediaLayout{}
	}
	return header
}
