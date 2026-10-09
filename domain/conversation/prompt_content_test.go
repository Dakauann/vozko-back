package conversation

import "testing"

func TestAMessageReachesTheModelTheSameWayItIsStored(t *testing.T) {
	if got := PromptContent("  oi, vocês abrem sábado?  \n", ""); got != "oi, vocês abrem sábado?" {
		t.Fatalf("text = %q", got)
	}
	if got := PromptContent("[Image] cardápio", "  Pizza grande R$ 50  "); got != "[Image] cardápio\n\nConteúdo extraído:\nPizza grande R$ 50" {
		t.Fatalf("media = %q", got)
	}
}

func TestAStoredMessageRendersItsExtractedContent(t *testing.T) {
	stored := &Message{Text: "[Document] contrato.pdf", Metadata: []byte(`{"` + ExtractedTextMetadataKey + `":"Cláusula 1"}`)}
	if got := stored.PromptContent(); got != PromptContent("[Document] contrato.pdf", "Cláusula 1") {
		t.Fatalf("replay = %q", got)
	}
	plain := &Message{Text: " olá ", Metadata: []byte(`{"other":1}`)}
	if got := plain.PromptContent(); got != "olá" {
		t.Fatalf("a message without extracted content renders its text, got %q", got)
	}
}
