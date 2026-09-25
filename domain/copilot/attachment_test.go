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
