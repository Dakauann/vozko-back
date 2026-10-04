package copilot

import (
	"strings"
	"testing"
)

func TestPromptWithAttachmentsListsEachFileWithItsMediaID(t *testing.T) {
	got := PromptWithAttachments("importe essa lista", []Attachment{{MediaID: "m1", Name: "clientes.csv", Kind: "document"}})
	if !strings.HasPrefix(got, "importe essa lista") || !strings.Contains(got, "clientes.csv (media_id: m1, tipo: document)") {
		t.Fatalf("prompt = %q", got)
	}
}

func TestPromptWithoutAttachmentsIsTheContent(t *testing.T) {
	if got := PromptWithAttachments("oi", nil); got != "oi" {
		t.Fatalf("prompt = %q", got)
	}
}

func TestImageURLsKeepsOnlyImagesWithAnAddress(t *testing.T) {
	got := ImageURLs([]Attachment{
		{MediaID: "m1", Kind: "image", URL: "https://cdn/logo.png"},
		{MediaID: "m2", Kind: "document", URL: "https://cdn/lista.csv"},
		{MediaID: "m3", Kind: "image"},
	})
	if len(got) != 1 || got[0] != "https://cdn/logo.png" {
		t.Fatalf("urls = %v", got)
	}
}
