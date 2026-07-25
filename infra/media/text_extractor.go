package media_infra

import (
	"fmt"
	"log"

	"vozko/domain/media"
)

type TextExtractorService struct {
	ocr     media.OCRService
	parsers []media.DocumentParser

	mimeIndex map[string]int
}

func NewTextExtractorService(ocr media.OCRService, parsers ...media.DocumentParser) *TextExtractorService {
	idx := make(map[string]int)
	for i, p := range parsers {
		for _, m := range p.SupportedMimeTypes() {
			idx[m] = i
		}
	}
	return &TextExtractorService{
		ocr:       ocr,
		parsers:   parsers,
		mimeIndex: idx,
	}
}

func (s *TextExtractorService) CanExtract(mimeType string) bool {
	mime := normalizeMime(mimeType)
	if imageMimeTypes[mime] && s.ocr != nil {
		return true
	}
	_, ok := s.mimeIndex[mime]
	return ok
}

func (s *TextExtractorService) ExtractText(data []byte, mimeType string) (*media.TextExtractionResult, error) {
	mime := normalizeMime(mimeType)

	if imageMimeTypes[mime] {
		if s.ocr == nil {
			return nil, nil
		}
		ocrResult, err := s.ocr.RecognizeText(data)
		if err != nil {
			log.Printf("[text-extractor] OCR failed for %s: %v", mime, err)
			return nil, fmt.Errorf("OCR failed: %w", err)
		}
		text := truncate(ocrResult.Text, maxExtractedTextLen)
		if text == "" {
			return nil, nil
		}
		return &media.TextExtractionResult{
			Text:   text,
			Source: "ocr",
		}, nil
	}

	idx, ok := s.mimeIndex[mime]
	if !ok {
		return nil, nil
	}
	content, err := s.parsers[idx].ParseDocument(data, mimeType)
	if err != nil {
		log.Printf("[text-extractor] document parsing failed for %s: %v", mime, err)
		return nil, fmt.Errorf("document parsing failed: %w", err)
	}
	if content == nil || content.Text == "" {
		return nil, nil
	}
	text := truncate(content.Text, maxExtractedTextLen)
	return &media.TextExtractionResult{
		Text:   text,
		Source: content.Format,
	}, nil
}

func truncate(s string, max int) string {
	if len(s) > max {
		return s[:max]
	}
	return s
}

var _ media.TextExtractor = (*TextExtractorService)(nil)
