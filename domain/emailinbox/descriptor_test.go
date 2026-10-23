package emailinbox

import (
	"testing"

	"vozko/domain/channel"
	"vozko/domain/emailsending"
	"vozko/domain/shared"
)

func TestDescriptor(t *testing.T) {
	d := Descriptor()
	if d.Kind != channel.KindEmail || d.EntryType != shared.EntryTypeEmail {
		t.Fatalf("descriptor identity = %s/%s", d.Kind, d.EntryType)
	}
	caps := d.Capabilities
	if caps.OutboundWindow != 0 || caps.OutboundBudget != 0 {
		t.Errorf("email has no reply clock and no budget: %+v", caps)
	}
	if caps.CanInitiateConversation || caps.SupportsTemplates || caps.SupportsReactions {
		t.Errorf("unexpected capability: %+v", caps)
	}
	for _, kind := range []channel.MediaKind{channel.MediaImage, channel.MediaVideo, channel.MediaAudio, channel.MediaDocument} {
		limit, ok := caps.MediaLimits[kind]
		if !ok || limit.MaxBytes != emailsending.MaxAttachmentBytes || len(limit.MIMETypes) != 0 {
			t.Errorf("%s attachments = %+v, %v, want any type up to the email attachment budget", kind, limit, ok)
		}
	}
	if len(caps.MediaLimits) != 4 {
		t.Errorf("media limits = %+v", caps.MediaLimits)
	}
	if caps.TextTooLong("ok") || !caps.TextTooLong(string(make([]byte, MaxReplyTextBytes+1))) {
		t.Errorf("reply text limit not enforced")
	}
}
