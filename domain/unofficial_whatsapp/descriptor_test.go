package unofficial_whatsapp

import (
	"strings"
	"testing"

	"vozko/domain/channel"
	"vozko/domain/shared"
)

// The descriptor is where this channel's capabilities are declared once for
// everything downstream. Each assertion below pins a difference from the Cloud
// API transport that would be a real bug if it drifted.
func TestDescriptorDeclaresTheTransportsDifferences(t *testing.T) {
	d := Descriptor()

	if d.Kind != channel.KindUnofficialWhatsApp {
		t.Errorf("kind = %q", d.Kind)
	}
	if d.EntryType != shared.EntryTypeUnofficialWhatsApp {
		t.Errorf("entry type = %q", d.EntryType)
	}

	caps := d.Capabilities

	// Cold outbound with no template is the reason the channel exists — and the
	// reason it can get a customer's number banned.
	if !caps.CanInitiateConversation {
		t.Error("this transport can open a conversation; declaring otherwise would hide the whole feature")
	}
	if caps.SupportsTemplates {
		t.Error("there are no templates on a linked-device session")
	}

	// No clock. What closes the composer is session state, a WhatsApp
	// restriction or a block, all resolved in the send adapter. A window here
	// would disable the composer on every conversation older than a day.
	if caps.OutboundWindow != 0 || caps.ExtendedWindow != 0 {
		t.Errorf("there is no messaging window: got %v/%v", caps.OutboundWindow, caps.ExtendedWindow)
	}

	// Real delivery callbacks, unlike Telegram. A status track that can fill
	// must be rendered.
	if !caps.SupportsReadReceipts {
		t.Error("this transport reports Delivered/Read; the CRM's status track is honest here")
	}

	// WhatsApp markup, not HTML: Telegram's signature format would print
	// literal tags into a customer's chat.
	if strings.Contains(caps.SignatureFormat, "<") {
		t.Errorf("signature format %q looks like HTML; WhatsApp renders *bold*", caps.SignatureFormat)
	}

	// Exactly one text measurement, or Capabilities.TextTooLong silently
	// applies the wrong one.
	if caps.MaxTextRunes == 0 || caps.MaxTextBytes != 0 {
		t.Errorf("expected a rune limit and no byte limit, got runes=%d bytes=%d",
			caps.MaxTextRunes, caps.MaxTextBytes)
	}
}

// WhatsApp splits at three buttons into a different message type and caps a
// list at ten rows. These are the platform's numbers, not ours, and the workflow
// editor shows them to authors before they publish.
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
	// The two styles must NOT collapse to one number: an author who picks
	// buttons gets three and one who picks a list gets ten, and the editor has
	// to be able to say so.
	if interactive.MaxOptionsButtons == interactive.MaxOptionsList {
		t.Error("buttons and lists have different caps on WhatsApp")
	}
	// List rows are the only option slot in the system with a description line.
	if !interactive.SupportsOptionDescriptions {
		t.Error("WhatsApp list rows carry a description line")
	}
}

// A nil MIME list means "any type accepted". Treating it as "accept nothing"
// would reject every document.
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

// The registry must accept this descriptor alongside the others: a duplicate
// kind or entry type is a boot failure, and it should surface here first.
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

// The inbox SQL fragments are interpolated into query text, so they must be
// code-authored constants with no bind placeholders of their own beyond the one
// the join template expects.
func TestDescriptorInboxSQLIsWellFormed(t *testing.T) {
	inbox := Descriptor().InboxSQL

	if inbox.EntryTable == "" || inbox.EntryJoin == "" {
		t.Fatal("inbox SQL is incomplete")
	}
	// Exactly one %[1]s verb, bound to the caller's entry-id column.
	if strings.Count(inbox.EntryJoin, "%[1]s") != 1 {
		t.Errorf("EntryJoin must carry exactly one entry-id verb: %q", inbox.EntryJoin)
	}
	// The automation quartet must be aliased exactly, or the projection breaks
	// the UNION for every channel, not just this one.
	for _, alias := range []string{
		"AS agent_id", "AS workflow_id",
		"AS agent_responses_enabled", "AS workflow_enabled",
	} {
		if !strings.Contains(inbox.AutomationFields, alias) {
			t.Errorf("AutomationFields is missing %q", alias)
		}
	}
}
