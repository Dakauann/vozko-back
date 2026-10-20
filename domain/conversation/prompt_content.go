package conversation

import (
	"encoding/json"
	"strings"
)

const (
	ExtractedTextMetadataKey = "extracted_text"
	extractedContentLabel    = "Conteúdo extraído:"
)

func PromptContent(text, extractedText string) string {
	content := strings.TrimSpace(text)
	if extracted := strings.TrimSpace(extractedText); extracted != "" {
		content += "\n\n" + extractedContentLabel + "\n" + extracted
	}
	return content
}

func ExtractedTextMetadata(extractedText string) []byte {
	encoded, err := json.Marshal(map[string]string{ExtractedTextMetadataKey: extractedText})
	if err != nil {
		return nil
	}
	return encoded
}

func (m *Message) ExtractedText() string {
	if m == nil {
		return ""
	}
	return metadataString(metadataFields(m.Metadata), ExtractedTextMetadataKey)
}

func (m *Message) PromptContent() string {
	if m == nil {
		return ""
	}
	return PromptContent(m.ReadableText(), m.ExtractedText())
}
