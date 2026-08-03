package telegram

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// loadFixture reads an update payload shaped exactly as Telegram delivers it.
//
// Testing against real payloads rather than hand-written structs is what catches
// the traps that make Telegram integrations fail: 52-bit ids rounded through a
// 32-bit field, photo size arrays read from the wrong end, a /start payload that
// is ordinary message text, and business messages whose direction is not implied
// by the update kind.
func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "updates", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return raw
}

func normalizeFixture(t *testing.T, name string) *Event {
	t.Helper()
	raw := loadFixture(t, name)
	update, err := DecodeUpdate(raw)
	if err != nil {
		t.Fatalf("DecodeUpdate(%s): %v", name, err)
	}
	ev := NormalizeUpdate("acct-1", update, json.RawMessage(raw))
	if ev == nil {
		t.Fatalf("NormalizeUpdate(%s) returned nil", name)
	}
	return ev
}

// Every fixture must normalize to the kind the handler dispatches on. A wrong
// kind is silent: the update is acked and the conversation simply never updates.
func TestNormalizeUpdateKinds(t *testing.T) {
	cases := []struct {
		fixture string
		want    EventKind
	}{
		{"text_message.json", EventInboundMessage},
		{"start_with_payload.json", EventInboundMessage},
		{"photo.json", EventInboundMessage},
		{"document_oversized.json", EventInboundMessage},
		{"voice.json", EventInboundMessage},
		{"sticker.json", EventInboundMessage},
		{"reply_to_message.json", EventInboundMessage},
		{"contact_shared.json", EventContactShared},
		{"contact_third_party.json", EventContactShared},
		{"edited_message.json", EventEditedMessage},
		{"callback_query.json", EventCallbackQuery},
		{"my_chat_member_blocked.json", EventBlocked},
		{"my_chat_member_unblocked.json", EventUnblocked},
		{"business_connection.json", EventBusinessConnection},
		{"business_message_inbound.json", EventInboundMessage},
		{"business_message_outbound.json", EventOutboundMessage},
		{"business_message_away.json", EventOutboundMessage},
		{"deleted_business_messages.json", EventDeletedMessages},
		{"group_message.json", EventInboundMessage},
		{"unknown_update.json", EventUnknown},
		{"large_ids.json", EventInboundMessage},
	}

	for _, tc := range cases {
		t.Run(tc.fixture, func(t *testing.T) {
			ev := normalizeFixture(t, tc.fixture)
			if ev.Kind != tc.want {
				t.Errorf("kind = %q, want %q", ev.Kind, tc.want)
			}
			if ev.UpdateID == 0 {
				t.Error("update_id must survive normalization: it is the dedup key")
			}
			if ev.IdempotencyKey == "" {
				t.Error("idempotency key must never be empty")
			}
		})
	}
}

// A Telegram id carries "at most 52 significant bits". Rounding one through a
// 32-bit field is the single most common Telegram integration bug, and it
// corrupts identity silently, the contact simply becomes a different person.
func TestLargeIdsSurviveDecoding(t *testing.T) {
	ev := normalizeFixture(t, "large_ids.json")

	if ev.UpdateID != 9007199254740991 {
		t.Errorf("update_id = %d, want 9007199254740991", ev.UpdateID)
	}
	if ev.From == nil || ev.From.ID != 4503599627370495 {
		t.Errorf("from.id = %v, want 4503599627370495", ev.From)
	}
	if ev.ChatID != 4503599627370495 {
		t.Errorf("chat.id = %d, want 4503599627370495", ev.ChatID)
	}
}

// The idempotency key must be scoped by account: update_id is unique per BOT,
// so two workspaces' bots collide on low ids on their first day.
func TestIdempotencyKeyIsScopedByAccount(t *testing.T) {
	raw := loadFixture(t, "text_message.json")
	update, err := DecodeUpdate(raw)
	if err != nil {
		t.Fatalf("DecodeUpdate: %v", err)
	}

	a := NormalizeUpdate("acct-a", update, raw)
	b := NormalizeUpdate("acct-b", update, raw)

	if a.IdempotencyKey == b.IdempotencyKey {
		t.Fatalf("two accounts share an idempotency key (%q), one bot's update would suppress another's",
			a.IdempotencyKey)
	}
}

// /start carries the only attribution this channel has, and Telegram delivers it
// as ordinary message text. Matching on the bot_command entity rather than a
// string prefix is what stops a message that merely mentions "/start" from being
// mistaken for one.
func TestStartPayloadExtraction(t *testing.T) {
	withPayload := normalizeFixture(t, "start_with_payload.json")
	if !withPayload.IsCommand {
		t.Error("a /start message must be flagged as a command")
	}
	if withPayload.StartPayload != "Zm9vYmFyMTIzNDU2Nzg" {
		t.Errorf("StartPayload = %q, want the deep-link token", withPayload.StartPayload)
	}

	bare := normalizeFixture(t, "start_without_payload.json")
	if !bare.IsCommand {
		t.Error("a bare /start is still a command")
	}
	if bare.StartPayload != "" {
		t.Errorf("StartPayload = %q, want empty for a bare /start", bare.StartPayload)
	}

	// A plain message must not be mistaken for a command.
	plain := normalizeFixture(t, "text_message.json")
	if plain.IsCommand || plain.StartPayload != "" {
		t.Error("an ordinary message must not parse as a /start")
	}
}

// In groups the command arrives as "/start@thebot"; stripping the suffix is what
// makes a group deep link work at all.
func TestStartCommandInGroupStripsBotSuffix(t *testing.T) {
	ev := normalizeFixture(t, "group_message.json")
	if !ev.IsCommand {
		t.Error("/start@vozko_bot must be recognised as the start command")
	}
	if ev.ChatType != ChatTypeSupergroup {
		t.Errorf("ChatType = %q, want supergroup", ev.ChatType)
	}
}

// A payload outside Telegram's alphabet is silently dropped by Telegram itself,
// so accepting one here would produce a link that opens an ordinary chat with no
// attribution, the hardest kind of bug to notice.
func TestValidDeepLinkToken(t *testing.T) {
	valid := []string{"abc", "A-Z_0-9", "Zm9vYmFyMTIzNDU2Nzg"}
	for _, tok := range valid {
		if !ValidDeepLinkToken(tok) {
			t.Errorf("ValidDeepLinkToken(%q) = false, want true", tok)
		}
	}

	invalid := []string{
		"",                       // empty
		"has space",              // space
		"has.dot",                // dot
		"café",                   // non-ASCII
		string(make([]byte, 65)), // over the 64-character ceiling
	}
	for _, tok := range invalid {
		if ValidDeepLinkToken(tok) {
			t.Errorf("ValidDeepLinkToken(%q) = true, want false", tok)
		}
	}
}

// Telegram sends every size of a photo, smallest first. Taking the first would
// store a 90px thumbnail in place of the customer's document.
func TestPhotoPicksTheLargestSize(t *testing.T) {
	ev := normalizeFixture(t, "photo.json")
	if len(ev.Attachments) != 1 {
		t.Fatalf("attachments = %d, want 1", len(ev.Attachments))
	}
	att := ev.Attachments[0]
	if att.FileID != "AgAClarge" {
		t.Errorf("file_id = %q, want the largest size", att.FileID)
	}
	if att.Kind != MediaPhoto {
		t.Errorf("kind = %q, want photo", att.Kind)
	}
	// The caption is the message body when there is no text.
	if ev.Text != "comprovante" {
		t.Errorf("Text = %q, want the caption", ev.Text)
	}
}

// The 20MB download ceiling is the channel's hardest product limit. Detecting it
// from the size Telegram already reported means the handler renders a real
// placeholder instead of attempting a fetch that can only fail.
func TestOversizedAttachmentIsFlaggedBeforeDownload(t *testing.T) {
	ev := normalizeFixture(t, "document_oversized.json")
	if len(ev.Attachments) != 1 {
		t.Fatalf("attachments = %d, want 1", len(ev.Attachments))
	}
	att := ev.Attachments[0]
	if !att.TooLarge {
		t.Errorf("a %d-byte file must be flagged TooLarge (ceiling is %d)", att.Size, MaxDownloadBytes)
	}
	// The MIME type and name are captured from the webhook, because getFile "may
	// not preserve the original file name and MIME type".
	if att.MIMEType != "application/pdf" || att.FileName != "contrato-assinado.pdf" {
		t.Errorf("mime/name = %q/%q, want them carried from the update", att.MIMEType, att.FileName)
	}

	// A small file must NOT be flagged, or every attachment would render as a
	// placeholder.
	small := normalizeFixture(t, "voice.json")
	if small.Attachments[0].TooLarge {
		t.Error("a 24KB voice note must not be flagged TooLarge")
	}
	if small.Attachments[0].Kind != MediaVoice {
		t.Errorf("kind = %q, want voice, the STT pipeline keys on it", small.Attachments[0].Kind)
	}
}

// A sticker has no useful body. Recording its emoji is the only thing that keeps
// the transcript from silently missing a turn.
func TestStickerCarriesItsEmoji(t *testing.T) {
	ev := normalizeFixture(t, "sticker.json")
	if len(ev.Attachments) != 1 || ev.Attachments[0].Emoji != "👍" {
		t.Errorf("sticker emoji not preserved: %+v", ev.Attachments)
	}
}

// business_message carries BOTH the customer's messages and the owner's own
// replies. sender_business_bot is the only thing in the payload that proves
// which, so misreading it would file the business's own replies as customer
// messages and corrupt every response-time metric.
func TestBusinessMessageDirection(t *testing.T) {
	inbound := normalizeFixture(t, "business_message_inbound.json")
	if inbound.Kind != EventInboundMessage {
		t.Errorf("kind = %q, want inbound", inbound.Kind)
	}
	if inbound.BusinessConnectionID != "BqhVQx8AAAAA" {
		t.Errorf("business_connection_id = %q, want it carried through, it is the only routing key",
			inbound.BusinessConnectionID)
	}

	outbound := normalizeFixture(t, "business_message_outbound.json")
	if outbound.Kind != EventOutboundMessage {
		t.Errorf("kind = %q, want outbound", outbound.Kind)
	}
	if outbound.IsAutomatic {
		t.Error("a normal reply must not be flagged automatic")
	}

	// An away/greeting message is Telegram's own automation, not an operator's
	// reply; labelling it as one would corrupt response-time metrics.
	away := normalizeFixture(t, "business_message_away.json")
	if !away.IsAutomatic {
		t.Error("is_from_offline must mark the message automatic")
	}
}

func TestDeletedBusinessMessages(t *testing.T) {
	ev := normalizeFixture(t, "deleted_business_messages.json")
	if len(ev.DeletedMessageIDs) != 2 {
		t.Fatalf("deleted ids = %v, want two", ev.DeletedMessageIDs)
	}
	if ev.ChatID != 5041234567 {
		t.Errorf("chat id = %d, deletions name a chat, never a contact", ev.ChatID)
	}
}

func TestBusinessConnectionRights(t *testing.T) {
	ev := normalizeFixture(t, "business_connection.json")
	if ev.Connection == nil {
		t.Fatal("connection must be carried through")
	}
	if !ev.Connection.IsEnabled {
		t.Error("is_enabled must be preserved: false means stop sending immediately")
	}
	if ev.Connection.Rights == nil || !ev.Connection.Rights.CanReply {
		t.Error("can_reply must be preserved: it is the whole outbound gate in business mode")
	}
}

// Only a SELF-share links an identity. A customer can forward anyone's contact
// card, and treating a third party's number as the sender's would merge two
// unrelated people in the CRM.
func TestSharedContactCarriesUserID(t *testing.T) {
	self := normalizeFixture(t, "contact_shared.json")
	if self.SharedContact == nil || self.From == nil {
		t.Fatal("shared contact and sender must both be present")
	}
	if self.SharedContact.UserID != self.From.ID {
		t.Error("fixture is not a self-share; the handler's identity check depends on it")
	}

	other := normalizeFixture(t, "contact_third_party.json")
	if other.SharedContact.UserID == other.From.ID {
		t.Error("fixture must be a third-party share so the handler's guard is exercised")
	}
}

func TestEditedMessageUsesEditDate(t *testing.T) {
	ev := normalizeFixture(t, "edited_message.json")
	if ev.Timestamp.Unix() != 1785312100 {
		t.Errorf("timestamp = %d, want edit_date 1785312100, using date would reorder the transcript",
			ev.Timestamp.Unix())
	}
}

func TestReplyToMessageIsCarried(t *testing.T) {
	ev := normalizeFixture(t, "reply_to_message.json")
	if ev.ReplyToMessageID != 4820 {
		t.Errorf("ReplyToMessageID = %d, want 4820", ev.ReplyToMessageID)
	}
}

func TestCallbackQueryCarriesIDAndData(t *testing.T) {
	ev := normalizeFixture(t, "callback_query.json")
	if ev.CallbackQueryID == "" {
		t.Error("callback query id must survive: an unanswered query spins the customer's button")
	}
	if ev.CallbackData != "negotiate:installments:3" {
		t.Errorf("CallbackData = %q, want the payload, button labels are display text", ev.CallbackData)
	}
}

// An unrecognised update must keep its raw payload. The Bot API adds update
// kinds several times a year, and silence would hide real traffic.
func TestUnknownUpdateKeepsRawPayload(t *testing.T) {
	ev := normalizeFixture(t, "unknown_update.json")
	if len(ev.Raw) == 0 {
		t.Error("an unknown update must carry its raw payload so it can be logged")
	}
}

func TestDecodeUpdateRejectsMalformed(t *testing.T) {
	for _, body := range []string{"", "{}", "not json", `{"message":{}}`} {
		if _, err := DecodeUpdate([]byte(body)); err == nil {
			t.Errorf("DecodeUpdate(%q) succeeded; a body with no update_id is not an update", body)
		}
	}
}

// message_id is unique only INSIDE a chat, so pairing it with the chat id is what
// makes the partial unique index on (entry_type, external_message_id) actually
// prevent duplicates instead of rejecting unrelated messages.
func TestProviderMessageIDRoundTrips(t *testing.T) {
	cases := [][2]int64{
		{5041234567, 4821},
		{-1001234567890, 55},
		{4503599627370495, 9007199254740991},
	}
	for _, c := range cases {
		id := ProviderMessageID(c[0], c[1])
		chatID, messageID, ok := ParseProviderMessageID(id)
		if !ok || chatID != c[0] || messageID != c[1] {
			t.Errorf("round trip of (%d,%d) via %q gave (%d,%d,%t)", c[0], c[1], id, chatID, messageID, ok)
		}
	}

	if _, _, ok := ParseProviderMessageID("not-an-id"); ok {
		t.Error("a malformed provider id must not parse")
	}
}

// Telegram warns that updates may arrive out of order. Arrival order is
// therefore not transcript order.
func TestSortByUpdateID(t *testing.T) {
	events := []*Event{{UpdateID: 30}, {UpdateID: 10}, {UpdateID: 20}}
	SortByUpdateID(events)
	for i, want := range []int64{10, 20, 30} {
		if events[i].UpdateID != want {
			t.Fatalf("position %d = %d, want %d", i, events[i].UpdateID, want)
		}
	}
}

// A tapped button reports an internal payload, not the words the contact read.
// Storing the payload as the message text put a raw id like "support" into the
// transcript AND handed it to the AI agent as the customer's message, where an
// agent whose tool description mentioned "Suporte" matched it and acted on it,
// re-showing the menu forever. The label is what a reader needs; the payload is
// what routing needs; they are not interchangeable.
func TestCallbackQueryUsesTheButtonLabelAsTheMessageText(t *testing.T) {
	ev := normalizeFixture(t, "callback_query_with_keyboard.json")

	if ev.Kind != EventCallbackQuery {
		t.Fatalf("kind = %s", ev.Kind)
	}
	if ev.Text != "Suporte" {
		t.Errorf("Text = %q, want the label the contact tapped", ev.Text)
	}
	// The payload must survive untouched, it is the branch key.
	if ev.CallbackData != "support" {
		t.Errorf("CallbackData = %q, want the payload", ev.CallbackData)
	}
}

// Telegram does not always echo the keyboard (an old message, an edited one).
// Falling back to the payload is worse than a label but never leaves the
// transcript blank.
func TestCallbackQueryFallsBackToThePayloadWithoutAKeyboard(t *testing.T) {
	ev := normalizeFixture(t, "callback_query.json")

	if ev.Text != "negotiate:installments:3" {
		t.Errorf("Text = %q, want the payload as the fallback", ev.Text)
	}
	if ev.CallbackData != "negotiate:installments:3" {
		t.Errorf("CallbackData = %q", ev.CallbackData)
	}
}

func TestLabelForMatchesAcrossRows(t *testing.T) {
	kb := &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{
		{{Text: "Suporte", CallbackData: "support"}},
		{{Text: "Vendas", CallbackData: "sales"}, {Text: "Financeiro", CallbackData: "finances"}},
	}}

	if got := kb.LabelFor("finances"); got != "Financeiro" {
		t.Errorf("LabelFor(finances) = %q", got)
	}
	if got := kb.LabelFor("unknown"); got != "" {
		t.Errorf("an unknown payload must report no label, got %q", got)
	}
	var nilKB *InlineKeyboardMarkup
	if got := nilKB.LabelFor("support"); got != "" {
		t.Errorf("a nil keyboard must be safe, got %q", got)
	}
}
