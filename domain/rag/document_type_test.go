package rag_test

import (
	"testing"

	"vozko/domain/rag"
)

func TestDocumentTypeFromName(t *testing.T) {
	for name, want := range map[string]rag.DocumentType{
		"Catalogo.PDF": rag.DocumentTypePDF,
		"faq.docx":     rag.DocumentTypeDocx,
		"notas.md":     rag.DocumentTypeMarkdown,
		"page.htm":     rag.DocumentTypeHTML,
		"dados.json":   rag.DocumentTypeJSON,
		"precos.csv":   rag.DocumentTypeText,
		"sem-extensao": rag.DocumentTypeText,
	} {
		if got := rag.DocumentTypeFromName(name); got != want {
			t.Fatalf("%s = %s", name, got)
		}
	}
}
