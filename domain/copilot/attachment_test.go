package copilot

import (
	"strings"
	"testing"
)

func TestPromptWithAttachmentsListsEachFileWithItsMediaID(t *testing.T) {
	got := PromptWithAttachments("importe essa lista", []Attachment{{MediaID: "m1", Name: "clientes.csv", Kind: "document"}}, false)
	if !strings.HasPrefix(got, "importe essa lista") || !strings.Contains(got, "clientes.csv (media_id: m1, tipo: document)") {
		t.Fatalf("prompt = %q", got)
	}
	if strings.Contains(got, BlindImagesNote) {
		t.Fatalf("a document needs no vision note: %q", got)
	}
}

func TestPromptWithoutAttachmentsIsTheContent(t *testing.T) {
	if got := PromptWithAttachments("oi", nil, false); got != "oi" {
		t.Fatalf("prompt = %q", got)
	}
}

func TestPromptTellsABlindModelItCannotSeeTheAttachedImages(t *testing.T) {
	images := []Attachment{{MediaID: "m2", Name: "logo.png", Kind: "image", URL: "https://cdn/logo.png"}}
	blind := PromptWithAttachments("o que tem no logo?", images, false)
	if !strings.Contains(blind, BlindImagesNote) {
		t.Fatalf("a model without vision must be told it cannot see the image: %q", blind)
	}
	if seeing := PromptWithAttachments("o que tem no logo?", images, true); strings.Contains(seeing, BlindImagesNote) {
		t.Fatalf("a vision model must not get the blind note: %q", seeing)
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
