package shortlink

import (
	"bytes"
	"strings"
	"testing"
)

func TestQRGenerate(t *testing.T) {
	g := NewQRGenerator()
	png, err := g.Generate("https://vx.co/r/abc", 256)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !bytes.HasPrefix(png, []byte{0x89, 'P', 'N', 'G'}) {
		t.Fatalf("output is not a PNG: %v", png[:4])
	}
}

func TestQRGenerateError(t *testing.T) {
	g := NewQRGenerator()
	if _, err := g.Generate(strings.Repeat("a", 10000), 256); err == nil {
		t.Fatal("expected error for content exceeding QR capacity")
	}
}
