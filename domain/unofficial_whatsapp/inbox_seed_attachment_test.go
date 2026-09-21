package unofficial_whatsapp

import (
	"errors"
	"testing"
)

func scriptWith(attachment *SeedAttachment) *SeedScript {
	return &SeedScript{
		Bodies:      []string{"Oi {{1}}, tudo bem?"},
		MaxMessages: 4,
		Attachment:  attachment,
	}
}

func TestSeedAttachmentAcceptsEveryMediaKind(t *testing.T) {
	for _, kind := range []MediaKind{
		MediaImage, MediaVideo, MediaAudio, MediaVoice, MediaDocument, MediaSticker,
	} {
		script := scriptWith(&SeedAttachment{MediaID: "media-1", Kind: kind})
		script.Normalize()
		if err := script.Validate(); err != nil {
			t.Fatalf("kind %q rejected: %v", kind, err)
		}
	}

	script := scriptWith(&SeedAttachment{MediaID: "media-1", Kind: MediaNone})
	script.Normalize()
	if err := script.Validate(); !errors.Is(err, ErrScriptMediaKindInvalid) {
		t.Fatalf("an unnamed kind was accepted: %v", err)
	}
	script = scriptWith(&SeedAttachment{MediaID: "media-1", Kind: "hologram"})
	script.Normalize()
	if err := script.Validate(); !errors.Is(err, ErrScriptMediaKindInvalid) {
		t.Fatalf("an invented kind was accepted: %v", err)
	}
}

func TestSeedAttachmentWithNoFileIsDropped(t *testing.T) {
	script := scriptWith(&SeedAttachment{MediaID: "   ", Kind: MediaImage})
	script.Normalize()

	if script.Attachment != nil {
		t.Fatalf("attachment = %+v, want it dropped", script.Attachment)
	}
	if err := script.Validate(); err != nil {
		t.Fatalf("the script itself should still be usable: %v", err)
	}
}

func TestSeedAttachmentNormalizesTheKind(t *testing.T) {
	script := scriptWith(&SeedAttachment{MediaID: "  media-1  ", Kind: "  IMAGE "})
	script.Normalize()

	if script.Attachment.MediaID != "media-1" || script.Attachment.Kind != MediaImage {
		t.Fatalf("attachment = %+v, want {media-1 image}", script.Attachment)
	}
}

func TestSeedAttachmentStillNeedsABody(t *testing.T) {
	script := &SeedScript{MaxMessages: 4, Attachment: &SeedAttachment{MediaID: "m-1", Kind: MediaImage}}
	script.Normalize()

	if err := script.Validate(); !errors.Is(err, ErrScriptBodyRequired) {
		t.Fatalf("Validate = %v, want ErrScriptBodyRequired", err)
	}
}

func TestSeedRequestCarriesAttachmentValidation(t *testing.T) {
	req := SeedRequest{
		WorkspaceID: "ws-1",
		Targets:     []SeedTarget{{Number: "5584999990001", Name: "Ana"}},
		Script:      scriptWith(&SeedAttachment{MediaID: "media-1", Kind: "pigeon"}),
	}
	req.Normalize()
	if err := req.Validate(); !errors.Is(err, ErrScriptMediaKindInvalid) {
		t.Fatalf("Validate = %v, want ErrScriptMediaKindInvalid", err)
	}
}
