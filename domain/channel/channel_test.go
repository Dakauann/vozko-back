package channel

import (
	"errors"
	"testing"
	"time"

	"vozko/domain/shared"
)

func descriptor(kind Kind, entryType shared.EntryType) *Descriptor {
	return &Descriptor{
		Kind:      kind,
		EntryType: entryType,
		Capabilities: Capabilities{
			MaxTextBytes:   1000,
			OutboundWindow: 24 * time.Hour,
		},
		InboxSQL: InboxSQL{EntryTable: string(entryType) + "_entries"},
	}
}

func TestRegistry_ResolvesByKindAndEntryType(t *testing.T) {
	want := descriptor(KindInstagram, shared.EntryTypeInstagram)

	registry, err := NewRegistry(want)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	byKind, err := registry.Get(KindInstagram)
	if err != nil {
		t.Fatalf("Get(instagram): %v", err)
	}
	if byKind != want {
		t.Error("Get returned a different descriptor")
	}

	byEntry, err := registry.ByEntryType(shared.EntryTypeInstagram)
	if err != nil {
		t.Fatalf("ByEntryType(instagram): %v", err)
	}
	if byEntry != want {
		t.Error("ByEntryType returned a different descriptor")
	}
}

// TestRegistry_UnregisteredChannelsError documents the migration state: WhatsApp and
// the support widget still carry their behaviour in per-channel switches, so the
// registry must report them as absent rather than returning a zero descriptor that
// would silently do the wrong thing.
func TestRegistry_UnregisteredChannelsError(t *testing.T) {
	registry, err := NewRegistry(descriptor(KindInstagram, shared.EntryTypeInstagram))
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	if _, err := registry.Get(KindWhatsApp); !errors.Is(err, ErrUnknownKind) {
		t.Errorf("Get(whatsapp) err = %v, want ErrUnknownKind", err)
	}
	if _, err := registry.Get(KindSupport); !errors.Is(err, ErrUnknownKind) {
		t.Errorf("Get(support) err = %v, want ErrUnknownKind", err)
	}
	if _, err := registry.ByEntryType(shared.EntryTypeWhatsApp); !errors.Is(err, ErrUnknownEntryType) {
		t.Errorf("ByEntryType(whatsapp) err = %v, want ErrUnknownEntryType", err)
	}
}

func TestRegistry_RejectsDuplicateKind(t *testing.T) {
	_, err := NewRegistry(
		descriptor(KindInstagram, shared.EntryTypeInstagram),
		descriptor(KindInstagram, shared.EntryTypeInstagram),
	)
	if !errors.Is(err, ErrKindAlreadySet) {
		t.Fatalf("err = %v, want ErrKindAlreadySet", err)
	}
}

func TestRegistry_IgnoresNilDescriptors(t *testing.T) {
	registry, err := NewRegistry(nil, descriptor(KindInstagram, shared.EntryTypeInstagram), nil)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if got := len(registry.All()); got != 1 {
		t.Fatalf("All() has %d descriptors, want 1", got)
	}
}

// TestRegistry_AllReturnsACopy: a caller must not be able to mutate the registry's
// internal ordering.
func TestRegistry_AllReturnsACopy(t *testing.T) {
	registry, err := NewRegistry(descriptor(KindInstagram, shared.EntryTypeInstagram))
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	all := registry.All()
	all[0] = nil

	if registry.All()[0] == nil {
		t.Error("All() exposed the internal slice")
	}
}

func TestMediaLimit_Allows(t *testing.T) {
	limit := MediaLimit{MaxBytes: 100, MIMETypes: []string{"image/png", "image/jpeg"}}

	if !limit.Allows("image/png") {
		t.Error("image/png should be allowed")
	}
	// gif is not an accepted Instagram image format.
	if limit.Allows("image/gif") {
		t.Error("image/gif should not be allowed")
	}
	if limit.Allows("") {
		t.Error("an empty mime type should not match")
	}
}

func TestKind_String(t *testing.T) {
	if KindInstagram.String() != "instagram" {
		t.Errorf("KindInstagram = %q, want instagram", KindInstagram.String())
	}
}
