package unofficial_whatsapp

import (
	"strings"
	"testing"

	"vozko/domain/channel"
	"vozko/domain/shared"
)

func TestDescriptorDeclaresTheTransportsDifferences(t *testing.T) {
	d := Descriptor()

	if d.Kind != channel.KindUnofficialWhatsApp {
		t.Errorf("kind = %q", d.Kind)
	}
	if d.EntryType != shared.EntryTypeUnofficialWhatsApp {
		t.Errorf("entry type = %q", d.EntryType)
	}

	caps := d.Capabilities

	if !caps.CanInitiateConversation {
		t.Error("this transport can open a conversation; declaring otherwise would hide the whole feature")
	}
	if caps.SupportsTemplates {
		t.Error("there are no templates on a linked-device session")
	}

	if caps.OutboundWindow != 0 || caps.ExtendedWindow != 0 {
		t.Errorf("there is no messaging window: got %v/%v", caps.OutboundWindow, caps.ExtendedWindow)
	}

	if !caps.SupportsReadReceipts {
		t.Error("this transport reports Delivered/Read; the CRM's status track is honest here")
	}

	if strings.Contains(caps.SignatureFormat, "<") {
		t.Errorf("signature format %q looks like HTML; WhatsApp renders *bold*", caps.SignatureFormat)
	}

	if caps.MaxTextRunes == 0 || caps.MaxTextBytes != 0 {
		t.Errorf("expected a rune limit and no byte limit, got runes=%d bytes=%d",
			caps.MaxTextRunes, caps.MaxTextBytes)
	}
}

func TestDescriptorInteractiveLimits(t *testing.T) {
	interactive := Descriptor().Capabilities.Interactive

	if !interactive.PresentsChoices() {
		t.Fatal("this channel presents choices")
	}
	if interactive.MaxOptionsButtons != MaxButtonOptions {
		t.Errorf("button cap = %d, want %d", interactive.MaxOptionsButtons, MaxButtonOptions)
	}
	if interactive.MaxOptionsList != MaxListOptions {
		t.Errorf("list cap = %d, want %d", interactive.MaxOptionsList, MaxListOptions)
	}
	if interactive.MaxOptionsButtons == interactive.MaxOptionsList {
		t.Error("buttons and lists have different caps on WhatsApp")
	}
	if !interactive.SupportsOptionDescriptions {
		t.Error("WhatsApp list rows carry a description line")
	}
}

func TestDescriptorDocumentsAcceptAnyType(t *testing.T) {
	limits := Descriptor().Capabilities.MediaLimits

	doc, ok := limits[channel.MediaDocument]
	if !ok {
		t.Fatal("no document limit declared")
	}
	if !doc.Allows("application/vnd.oasis.opendocument.text") {
		t.Error("documents must accept any MIME type")
	}
	if doc.MaxBytes <= 0 {
		t.Error("a document limit of zero would reject every file")
	}

	image, ok := limits[channel.MediaImage]
	if !ok {
		t.Fatal("no image limit declared")
	}
	if image.Allows("application/pdf") {
		t.Error("the image kind must not accept a PDF")
	}
}

func TestDescriptorRegisters(t *testing.T) {
	registry, err := channel.NewRegistry(Descriptor())
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if _, err := registry.Get(channel.KindUnofficialWhatsApp); err != nil {
		t.Errorf("Get by kind: %v", err)
	}
	if _, err := registry.ByEntryType(shared.EntryTypeUnofficialWhatsApp); err != nil {
		t.Errorf("ByEntryType: %v", err)
	}
}

func TestDescriptorInboxSQLIsWellFormed(t *testing.T) {
	inbox := Descriptor().InboxSQL

	if inbox.EntryTable == "" || inbox.EntryJoin == "" {
		t.Fatal("inbox SQL is incomplete")
	}
	if strings.Count(inbox.EntryJoin, "%[1]s") != 1 {
		t.Errorf("EntryJoin must carry exactly one entry-id verb: %q", inbox.EntryJoin)
	}
	for _, alias := range []string{
		"AS agent_id", "AS workflow_id",
		"AS agent_responses_enabled", "AS workflow_enabled",
	} {
		if !strings.Contains(inbox.AutomationFields, alias) {
			t.Errorf("AutomationFields is missing %q", alias)
		}
	}
}
