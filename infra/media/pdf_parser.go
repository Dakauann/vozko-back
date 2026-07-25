package media_infra

import (
	"bytes"
	"fmt"
	"log"
	"strings"

	"github.com/ledongthuc/pdf"

	"vozko/domain/media"
)

type PDFParser struct{}

func NewPDFParser() *PDFParser { return &PDFParser{} }

func (p *PDFParser) SupportedMimeTypes() []string {
	return []string{"application/pdf"}
}

func (p *PDFParser) ParseDocument(data []byte, mimeType string) (*media.DocumentContent, error) {
	if normalizeMime(mimeType) != "application/pdf" {
		return nil, nil
	}

	reader := bytes.NewReader(data)
	pdfReader, err := pdf.NewReader(reader, int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("failed to open PDF: %w", err)
	}

	numPages := pdfReader.NumPage()
	var sb strings.Builder

	for i := 1; i <= numPages; i++ {
		page := pdfReader.Page(i)
		if page.V.IsNull() {
			continue
		}

		text, err := page.GetPlainText(nil)
		if err != nil {
			log.Printf("[pdf-parser] failed to extract text from page %d: %v", i, err)
			continue
		}

		trimmed := strings.TrimSpace(text)
		if trimmed != "" {
			if sb.Len() > 0 {
				sb.WriteString("\n\n")
			}
			sb.WriteString(trimmed)
		}

		if sb.Len() > maxExtractedTextLen {
			break
		}
	}

	return &media.DocumentContent{
		Text:      sb.String(),
		PageCount: numPages,
		Format:    "pdf",
	}, nil
}

var _ media.DocumentParser = (*PDFParser)(nil)
