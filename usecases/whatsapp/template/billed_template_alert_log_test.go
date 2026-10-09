package template_usecase

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"
)

func TestBilledSend_AlertLogLineUsesPlainPunctuation(t *testing.T) {
	var out bytes.Buffer
	previousOutput, previousFlags := log.Writer(), log.Flags()
	log.SetOutput(&out)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(previousOutput)
		log.SetFlags(previousFlags)
	})

	(&billedTemplateSendUseCase{}).alert(context.Background(), "WhatsApp template refund failed", "attempt=a1")

	if got, want := strings.TrimSpace(out.String()), "[billed-template-send] WhatsApp template refund failed: attempt=a1"; got != want {
		t.Fatalf("log line = %q, want %q", got, want)
	}
}
