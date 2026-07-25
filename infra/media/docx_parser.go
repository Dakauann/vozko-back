package media_infra

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"vozko/domain/media"
)

type DOCXParser struct{}

func NewDOCXParser() *DOCXParser { return &DOCXParser{} }

func (p *DOCXParser) SupportedMimeTypes() []string {
	return []string{
		"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	}
}

func (p *DOCXParser) ParseDocument(data []byte, mimeType string) (*media.DocumentContent, error) {
	mime := normalizeMime(mimeType)
	if mime != "application/vnd.openxmlformats-officedocument.wordprocessingml.document" {
		return nil, nil
	}

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("failed to open DOCX archive: %w", err)
	}

	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("failed to open document.xml: %w", err)
		}
		defer rc.Close()

		xmlData, err := io.ReadAll(rc)
		if err != nil {
			return nil, fmt.Errorf("failed to read document.xml: %w", err)
		}

		var body docxBody
		if err := xml.Unmarshal(xmlData, &body); err != nil {
			return nil, fmt.Errorf("failed to parse document.xml: %w", err)
		}

		var sb strings.Builder
		for _, p := range body.Paragraphs {
			var line strings.Builder
			for _, r := range p.Runs {
				line.WriteString(r.Text)
			}
			text := strings.TrimSpace(line.String())
			if text != "" {
				if sb.Len() > 0 {
					sb.WriteString("\n")
				}
				sb.WriteString(text)
			}
		}

		return &media.DocumentContent{
			Text:   sb.String(),
			Format: "docx",
		}, nil
	}

	return nil, fmt.Errorf("word/document.xml not found in DOCX archive")
}

type docxBody struct {
	Paragraphs []docxParagraph `xml:"body>p"`
}

type docxParagraph struct {
	Runs []docxRun `xml:"r"`
}

type docxRun struct {
	Text string `xml:"t"`
}

var _ media.DocumentParser = (*DOCXParser)(nil)
