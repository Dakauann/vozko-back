package rag_test

import (
	"encoding/base64"
	"testing"

	"vozko/domain/rag"
)

func TestContentSizeCountsTheDecodedFile(t *testing.T) {
	for _, raw := range []string{"", "a", "ab", "abc", "%PDF-1.7 tabela de preços"} {
		encoded := base64.StdEncoding.EncodeToString([]byte(raw))
		if got := rag.ContentSize(encoded, map[string]string{"encoding": rag.EncodingBase64}); got != int64(len(raw)) {
			t.Fatalf("%q: size %d, want %d", raw, got, len(raw))
		}
	}
}

func TestContentSizeOfPlainTextIsItsBytes(t *testing.T) {
	if got := rag.ContentSize("preço", nil); got != int64(len("preço")) {
		t.Fatalf("size %d", got)
	}
}

func TestSizeInMB(t *testing.T) {
	if got := rag.SizeInMB(3 * 1024 * 1024 / 2); got != 1.5 {
		t.Fatalf("mb = %v", got)
	}
}
