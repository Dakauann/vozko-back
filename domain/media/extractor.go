package media

type OCRResult struct {
	Text       string
	Confidence float64
}

type OCRService interface {
	RecognizeText(imageData []byte) (*OCRResult, error)
}

type DocumentContent struct {
	Text      string
	PageCount int
	Format    string
}

type DocumentParser interface {
	ParseDocument(data []byte, mimeType string) (*DocumentContent, error)

	SupportedMimeTypes() []string
}

type TextExtractionResult struct {
	Text string

	Source string
}

type TextExtractor interface {
	ExtractText(data []byte, mimeType string) (*TextExtractionResult, error)

	CanExtract(mimeType string) bool
}
