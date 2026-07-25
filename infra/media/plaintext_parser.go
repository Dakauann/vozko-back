package media_infra

import (
	"vozko/domain/media"
)

type PlainTextParser struct{}

func NewPlainTextParser() *PlainTextParser { return &PlainTextParser{} }

func (p *PlainTextParser) SupportedMimeTypes() []string {
	return []string{
		"text/plain",
		"text/csv",
		"text/html",
		"application/json",
		"application/xml",
		"text/xml",
	}
}

func (p *PlainTextParser) ParseDocument(data []byte, mimeType string) (*media.DocumentContent, error) {
	mime := normalizeMime(mimeType)
	supported := false
	for _, m := range p.SupportedMimeTypes() {
		if m == mime {
			supported = true
			break
		}
	}
	if !supported {
		return nil, nil
	}

	text := string(data)
	if len(text) > maxExtractedTextLen {
		text = text[:maxExtractedTextLen]
	}

	format := "txt"
	switch mime {
	case "text/csv":
		format = "csv"
	case "text/html":
		format = "html"
	case "application/json":
		format = "json"
	case "application/xml", "text/xml":
		format = "xml"
	}

	return &media.DocumentContent{
		Text:   text,
		Format: format,
	}, nil
}

var _ media.DocumentParser = (*PlainTextParser)(nil)
