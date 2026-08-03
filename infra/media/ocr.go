package media_infra

import (
	"bytes"
	"fmt"
	"log"
	"os/exec"
	"strings"

	"vozko/domain/media"
)

type TesseractOCR struct {
	languages string
}

func NewTesseractOCR(langs string) *TesseractOCR {
	if langs == "" {
		langs = "por+eng"
	}
	return &TesseractOCR{languages: langs}
}

func (t *TesseractOCR) RecognizeText(imageData []byte) (*media.OCRResult, error) {
	if len(imageData) == 0 {
		return nil, fmt.Errorf("empty image data")
	}

	args := []string{"stdin", "stdout", "-l", t.languages}
	cmd := exec.Command("tesseract", args...)
	cmd.Stdin = bytes.NewReader(imageData)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		stderrText := strings.TrimSpace(stderr.String())
		log.Printf("[ocr] tesseract failed (lang=%s): %v, %s", t.languages, err, stderrText)
		return nil, fmt.Errorf("tesseract OCR failed: %w", err)
	}

	text := strings.TrimSpace(stdout.String())
	return &media.OCRResult{
		Text: text,
	}, nil
}

var _ media.OCRService = (*TesseractOCR)(nil)
