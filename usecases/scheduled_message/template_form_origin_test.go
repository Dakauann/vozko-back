package scheduled_message_usecase

import (
	"context"
	"testing"

	wf "vozko/domain/whatsappform"
)

func TestDispatchATemplateTellsTheFormSendItWasScheduled(t *testing.T) {
	f := newDispatchFixture(t)
	f.repo.put(templateMessage("sched-1", fixedNow))
	f.windows.set(false, nil)

	if err := f.uc.Execute(context.Background(), "sched-1"); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	sent := f.templates.sends[0]
	if sent.Origin != wf.OriginTemplateScheduled || sent.OriginRef != "sched-1" {
		t.Fatalf("origin %q ref %q, want %q for sched-1", sent.Origin, sent.OriginRef, wf.OriginTemplateScheduled)
	}
}
