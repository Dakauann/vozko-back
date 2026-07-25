package media_infra

import "strings"

const maxExtractedTextLen = 8000

func normalizeMime(mime string) string {
	if idx := strings.Index(mime, ";"); idx != -1 {
		mime = mime[:idx]
	}
	return strings.TrimSpace(strings.ToLower(mime))
}

var imageMimeTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/webp": true,
	"image/gif":  true,
	"image/bmp":  true,
	"image/tiff": true,
}
