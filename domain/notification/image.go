package notification

import (
	"net/http"
	"strings"
)

const MaxImageBytes = 5 << 20

var imageExtensions = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

type Image struct {
	MIME string
	Ext  string
}

func ClassifyImage(data []byte) (Image, error) {
	if len(data) == 0 {
		return Image{}, ErrImageEmpty
	}
	if len(data) > MaxImageBytes {
		return Image{}, ErrImageTooLarge
	}
	mime := strings.SplitN(http.DetectContentType(data), ";", 2)[0]
	ext, ok := imageExtensions[mime]
	if !ok {
		return Image{}, ErrImageType
	}
	return Image{MIME: mime, Ext: ext}, nil
}
