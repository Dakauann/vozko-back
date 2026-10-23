package mercadolivre

import (
	"strings"
	"testing"

	"vozko/domain/channel"
	"vozko/domain/shared"
)

func TestDescriptor(t *testing.T) {
	d := Descriptor()
	if d.Kind != channel.KindMercadoLivre || d.EntryType != shared.EntryTypeMercadoLivre {
		t.Fatalf("kind %q entry %q", d.Kind, d.EntryType)
	}
	c := d.Capabilities
	if c.CanInitiateConversation || c.SupportsTemplates || c.SupportsReactions || c.SupportsTypingIndicator || c.SupportsRichText {
		t.Fatalf("capabilities claim something Mercado Livre lacks: %+v", c)
	}
	if !c.SupportsReadReceipts {
		t.Fatal("read receipts come in through the messages read action")
	}
	if c.MaxTextRunes != MaxMessageRunes || c.MaxTextBytes != 0 {
		t.Fatalf("text limits = %d runes, %d bytes", c.MaxTextRunes, c.MaxTextBytes)
	}
	if c.OutboundWindow != 0 || c.ExtendedWindow != 0 || c.HumanAgentWindow {
		t.Fatal("the window comes from the thread status, never a clock")
	}
	if c.OutboundBudget != 0 {
		t.Fatal("the budget is per conversation, the adapter applies it with WithOutboundBudget")
	}
	if c.SignatureFormat != "%s:\n%s" {
		t.Fatalf("signature = %q", c.SignatureFormat)
	}
	if c.Interactive.PresentsChoices() || c.Interactive.MaxLabelRunes != 0 || c.Interactive.MaxPayloadBytes != 0 {
		t.Fatalf("interactive = %+v", c.Interactive)
	}
	if len(c.MediaLimits) != 2 {
		t.Fatalf("media kinds = %v", c.MediaLimits)
	}
	image := c.MediaLimits[channel.MediaImage]
	doc := c.MediaLimits[channel.MediaDocument]
	if image.MaxBytes != 25<<20 || doc.MaxBytes != 25<<20 {
		t.Fatalf("media sizes = %d, %d", image.MaxBytes, doc.MaxBytes)
	}
	if !image.Allows("image/jpeg") || !image.Allows("image/png") || image.Allows("image/gif") {
		t.Fatalf("image types = %v", image.MIMETypes)
	}
	if !doc.Allows("application/pdf") || !doc.Allows("text/plain") || doc.Allows("application/zip") {
		t.Fatalf("document types = %v", doc.MIMETypes)
	}
	if _, ok := c.MediaLimits[channel.MediaVideo]; ok {
		t.Fatal("video is not accepted")
	}
	if _, ok := c.MediaLimits[channel.MediaAudio]; ok {
		t.Fatal("audio is not accepted")
	}
	if d.InboxSQL.EntryTable != "mercadolivre_conversations" || !strings.Contains(d.InboxSQL.EntryJoin, "mercadolivre_accounts") {
		t.Fatalf("inbox sql = %+v", d.InboxSQL)
	}
}

func TestInboxNamesTheAccountBySellerNameFirst(t *testing.T) {
	want := "COALESCE(NULLIF(mla.display_name, ''), NULLIF(mla.nickname, ''), mla.ml_user_id)"
	if got := Descriptor().InboxSQL.ContainerNameField; got != want {
		t.Fatalf("container name = %q", got)
	}
	if got := AccountNameSQL(""); got != "COALESCE(NULLIF(display_name, ''), NULLIF(nickname, ''), ml_user_id)" {
		t.Fatalf("unqualified account name = %q", got)
	}
}

func TestDescriptorCarriesTheCharsetRule(t *testing.T) {
	c := Descriptor().Capabilities
	if len(c.ProviderTextRules) != 1 {
		t.Fatalf("provider rules = %d", len(c.ProviderTextRules))
	}
	violations := c.TextViolations("Total 10\U000020AC " + strings.Repeat("a", 350))
	codes := map[string]bool{}
	for _, v := range violations {
		codes[v.Code] = true
	}
	if !codes[channel.TextViolationMaxRunes] || !codes[TextViolationCharset] {
		t.Fatalf("violations = %+v", violations)
	}
	if got := c.TextViolations("Ol\U000000E1 \U0001F44D"); len(got) != 0 {
		t.Fatalf("valid text flagged: %+v", got)
	}
	instructions := strings.Join(c.TextInstructions(), " ")
	if !strings.Contains(instructions, "ISO-8859-1") {
		t.Fatalf("instructions = %q", instructions)
	}
}

func TestCharsetRuleNamesTheOffendingCharacters(t *testing.T) {
	v, broken := CharsetTextRule().Check("a\U0000201Cb\U0000201D \U000020AC")
	if !broken || v.Code != TextViolationCharset {
		t.Fatalf("violation = %+v, %v", v, broken)
	}
	for _, r := range []string{"\U0000201C", "\U0000201D", "\U000020AC"} {
		if !strings.Contains(v.Message, r) {
			t.Fatalf("message %q misses %q", v.Message, r)
		}
	}
	if _, broken := CharsetTextRule().Check(""); broken {
		t.Fatal("emptiness is not a charset problem")
	}
}
