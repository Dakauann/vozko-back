package rag

import (
	"path"
	"strings"
)

var documentTypeByExtension = map[string]DocumentType{
	"pdf":  DocumentTypePDF,
	"docx": DocumentTypeDocx,
	"doc":  DocumentTypeDocx,
	"md":   DocumentTypeMarkdown,
	"html": DocumentTypeHTML,
	"htm":  DocumentTypeHTML,
	"json": DocumentTypeJSON,
}

func DocumentTypeFromName(name string) DocumentType {
	if t, ok := documentTypeByExtension[strings.ToLower(strings.TrimPrefix(path.Ext(name), "."))]; ok {
		return t
	}
	return DocumentTypeText
}
