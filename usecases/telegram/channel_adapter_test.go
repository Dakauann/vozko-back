package telegram

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	tgdomain "vozko/domain/telegram"
)

func botAccount() *tgdomain.Account {
	return &tgdomain.Account{
		ID:          "acct-1",
		WorkspaceID: "ws-1",
		Mode:        tgdomain.ModeBot,
		BotUserID:   77777,
		BotUsername: "vozko_bot",
		BotToken:    "77777:secret",
		Status:      tgdomain.StatusActive,
	}
}

func businessAccount(canReply bool) *tgdomain.Account {
	a := botAccount()
	a.Mode = tgdomain.ModeBusiness
	connectionID := "BqhVQx8AAAAA"
	a.BusinessConnectionID = &connectionID
	a.BusinessEnabled = true
	a.BusinessRights = &tgdomain.BusinessRights{CanReply: canReply, CanReadMessages: true}
	return a
}

func conversationFor(chatID int64) *tgdomain.Conversation {
	return &tgdomain.Conversation{
		ID:          "conv-1",
		WorkspaceID: "ws-1",
		AccountID:   "acct-1",
		ContactID:   "contact-1",
		TGChatID:    chatID,
		ChatType:    tgdomain.ChatTypePrivate,
	}
}

// newAdapter wires the adapter with fakes and returns the pieces a test asserts
// against.
func newAdapter(
	account *tgdomain.Account,
	conv *tgdomain.Conversation,
	contact *tgdomain.Contact,
) (conversation.ChannelAdapter, *fakeAccounts, *fakeContacts, *fakeConversations, *fakeBotAPI, *fakeFileCache) {
	accounts := &fakeAccounts{
		FindByIDFn: func(context.Context, string) (*tgdomain.Account, error) { return account, nil },
	}
	contacts := &fakeContacts{
		FindByIDFn: func(context.Context, string) (*tgdomain.Contact, error) { return contact, nil },
	}
	conversations := &fakeConversations{
		FindByIDFn: func(context.Context, string) (*tgdomain.Conversation, error) { return conv, nil },
	}
	api := &fakeBotAPI{}
	files := newFakeFileCache()

	return NewChannelAdapter(accounts, contacts, conversations, files, api),
		accounts, contacts, conversations, api, files
}

func entryContext(conv *tgdomain.Conversation, lastInbound *time.Time) *conversation.EntryContext {
	return &conversation.EntryContext{
		EntryID:       conv.ID,
		EntryType:     shared.EntryTypeTelegram,
		WorkspaceID:   conv.WorkspaceID,
		AccountID:     conv.AccountID,
		ContactID:     conv.ContactID,
		ContactRef:    "5041234567",
		LastInboundAt: lastInbound,
	}
}

// Bot mode has NO messaging window. Applying Instagram's 24h clock here would
// disable the composer on every conversation older than a day, for no reason the
// platform imposes.
func TestWindowStateBotModeHasNoClock(t *testing.T) {
	conv := conversationFor(5041234567)
	contact := &tgdomain.Contact{ID: "contact-1", TGUserID: 5041234567}
	adapter, _, _, _, _, _ := newAdapter(botAccount(), conv, contact)

	// An inbound from a week ago must still leave the window open.
	old := time.Now().UTC().Add(-7 * 24 * time.Hour)
	open, expires, err := adapter.WindowState(context.Background(), entryContext(conv, &old))
	if err != nil {
		t.Fatalf("WindowState: %v", err)
	}
	if !open {
		t.Error("bot mode has no window; a week-old conversation is still repliable")
	}
	// A nil expiry is what tells the UI "this is not a clock".
	if expires != nil {
		t.Errorf("expiry = %v, want nil, there is no moment at which bot mode reopens", expires)
	}
}

// The real outbound gate in bot mode is reachability: the customer blocking the
// bot. It never reopens on its own, so the UI copy has to differ from a window.
func TestWindowStateBotModeClosesWhenBlocked(t *testing.T) {
	conv := conversationFor(5041234567)
	contact := &tgdomain.Contact{ID: "contact-1", TGUserID: 5041234567, Blocked: true}
	adapter, _, _, _, _, _ := newAdapter(botAccount(), conv, contact)

	open, expires, err := adapter.WindowState(context.Background(), entryContext(conv, nil))
	if err != nil {
		t.Fatalf("WindowState: %v", err)
	}
	if open {
		t.Error("a blocked contact closes the composer")
	}
	if expires != nil {
		t.Error("being blocked is not a clock; there is no expiry to show")
	}
}

// Business mode reintroduces Instagram's exact 24h rule, because can_reply is
// defined as "private chats that had incoming messages in the last 24 hours".
func TestWindowStateBusinessModeUsesTheClock(t *testing.T) {
	conv := conversationFor(5041234567)
	contact := &tgdomain.Contact{ID: "contact-1", TGUserID: 5041234567}
	adapter, _, _, _, _, _ := newAdapter(businessAccount(true), conv, contact)

	fresh := time.Now().UTC().Add(-time.Hour)
	open, expires, err := adapter.WindowState(context.Background(), entryContext(conv, &fresh))
	if err != nil {
		t.Fatalf("WindowState: %v", err)
	}
	if !open || expires == nil {
		t.Fatal("a recent inbound leaves the business window open, with an expiry to show")
	}

	stale := time.Now().UTC().Add(-25 * time.Hour)
	if open, _, _ := adapter.WindowState(context.Background(), entryContext(conv, &stale)); open {
		t.Error("a 25-hour-old inbound closes the business window")
	}
}

// A revoked right is not a clock either: the UI must say "permission revoked",
// which it can only do if no expiry is reported.
func TestWindowStateBusinessModeWithoutCanReply(t *testing.T) {
	conv := conversationFor(5041234567)
	contact := &tgdomain.Contact{ID: "contact-1"}
	adapter, _, _, _, _, _ := newAdapter(businessAccount(false), conv, contact)

	fresh := time.Now().UTC().Add(-time.Minute)
	open, expires, err := adapter.WindowState(context.Background(), entryContext(conv, &fresh))
	if err != nil {
		t.Fatalf("WindowState: %v", err)
	}
	if open {
		t.Error("without can_reply the composer is closed even inside the 24h window")
	}
	if expires != nil {
		t.Error("a revoked right has no expiry")
	}
}

// Telegram counts CHARACTERS. A byte limit would reject an emoji-heavy message
// Telegram accepts.
func TestSendTextEnforcesRuneLimit(t *testing.T) {
	conv := conversationFor(5041234567)
	contact := &tgdomain.Contact{ID: "contact-1"}
	adapter, _, _, _, api, _ := newAdapter(botAccount(), conv, contact)

	// Exactly at the limit, in 4-byte runes: ~16KB of bytes, 4096 characters.
	atLimit := strings.Repeat("😀", tgdomain.MaxTextRunes)
	if _, err := adapter.SendText(context.Background(), entryContext(conv, nil),
		conversation.SendTextRequest{Body: atLimit}); err != nil {
		t.Fatalf("a 4096-character message must be accepted: %v", err)
	}
	if len(api.SentText) != 1 {
		t.Fatal("the send should have reached the API")
	}

	over := atLimit + "x"
	_, err := adapter.SendText(context.Background(), entryContext(conv, nil),
		conversation.SendTextRequest{Body: over})
	if !errors.Is(err, tgdomain.ErrTextTooLong) {
		t.Errorf("err = %v, want ErrTextTooLong", err)
	}
}

// HTML is the parse mode, and interpolated customer text must be escaped or a
// stray "<" fails the whole send.
func TestSendTextEscapesHTML(t *testing.T) {
	conv := conversationFor(5041234567)
	contact := &tgdomain.Contact{ID: "contact-1"}
	adapter, _, _, _, api, _ := newAdapter(botAccount(), conv, contact)

	if _, err := adapter.SendText(context.Background(), entryContext(conv, nil),
		conversation.SendTextRequest{Body: `5 < 10 & "quoted"`}); err != nil {
		t.Fatalf("SendText: %v", err)
	}

	sent := api.SentText[0]
	if sent.ParseMode != "HTML" {
		t.Errorf("ParseMode = %q, want HTML", sent.ParseMode)
	}
	if strings.Contains(sent.Text, "<") || strings.Contains(sent.Text, "&\"") {
		t.Errorf("text was not escaped: %q", sent.Text)
	}
	if !strings.Contains(sent.Text, "&lt;") {
		t.Errorf("expected an escaped '<' in %q", sent.Text)
	}
}

// The provider id is known synchronously, Telegram answers a send with the full
// Message, so there is no echo webhook to reconcile against. It must pair the
// chat id with the message id, because message_id is unique only inside a chat.
func TestSendTextReturnsCompositeProviderID(t *testing.T) {
	conv := conversationFor(5041234567)
	contact := &tgdomain.Contact{ID: "contact-1"}
	adapter, _, _, conversations, _, _ := newAdapter(botAccount(), conv, contact)

	outcome, err := adapter.SendText(context.Background(), entryContext(conv, nil),
		conversation.SendTextRequest{Body: "oi"})
	if err != nil {
		t.Fatalf("SendText: %v", err)
	}

	chatID, messageID, ok := tgdomain.ParseProviderMessageID(outcome.ProviderMessageID)
	if !ok || chatID != 5041234567 || messageID != 999 {
		t.Errorf("ProviderMessageID = %q, want chat:message", outcome.ProviderMessageID)
	}
	if len(conversations.OutboundWrites) != 1 {
		t.Error("a successful send must advance the agent clock")
	}
}

// A reply must quote by message id, which is the half of the composite the
// provider actually understands.
func TestSendTextReplyUsesMessageIDOnly(t *testing.T) {
	conv := conversationFor(5041234567)
	contact := &tgdomain.Contact{ID: "contact-1"}
	adapter, _, _, _, api, _ := newAdapter(botAccount(), conv, contact)

	if _, err := adapter.SendText(context.Background(), entryContext(conv, nil),
		conversation.SendTextRequest{
			Body:                     "sim",
			ReplyToProviderMessageID: tgdomain.ProviderMessageID(77777, 5041234567, 4820),
		}); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if api.SentText[0].ReplyToMessageID != 4820 {
		t.Errorf("ReplyToMessageID = %d, want 4820", api.SentText[0].ReplyToMessageID)
	}
}

// Business mode must send on behalf of the connection the conversation ARRIVED
// through, not whatever the account currently holds: an account can be
// reconnected, and answering on the wrong connection would surface the reply
// under a different identity.
func TestSendTextCarriesBusinessConnection(t *testing.T) {
	conv := conversationFor(5041234567)
	convConnection := "conversation-connection"
	conv.BusinessConnectionID = &convConnection
	contact := &tgdomain.Contact{ID: "contact-1"}

	adapter, _, _, _, api, _ := newAdapter(businessAccount(true), conv, contact)

	fresh := time.Now().UTC().Add(-time.Minute)
	if _, err := adapter.SendText(context.Background(), entryContext(conv, &fresh),
		conversation.SendTextRequest{Body: "oi"}); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if api.SentText[0].BusinessConnectionID != convConnection {
		t.Errorf("BusinessConnectionID = %q, want the conversation's own connection",
			api.SentText[0].BusinessConnectionID)
	}
}

// Bot mode must NOT carry a connection id, or Telegram rejects the send.
func TestSendTextOmitsBusinessConnectionInBotMode(t *testing.T) {
	conv := conversationFor(5041234567)
	contact := &tgdomain.Contact{ID: "contact-1"}
	adapter, _, _, _, api, _ := newAdapter(botAccount(), conv, contact)

	if _, err := adapter.SendText(context.Background(), entryContext(conv, nil),
		conversation.SendTextRequest{Body: "oi"}); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if api.SentText[0].BusinessConnectionID != "" {
		t.Error("bot mode must not send a business_connection_id")
	}
}

// A blocked contact must fail with the window-closed sentinel so the composer
// explains itself rather than surfacing a raw provider error.
func TestSendTextRefusesWhenBlocked(t *testing.T) {
	conv := conversationFor(5041234567)
	contact := &tgdomain.Contact{ID: "contact-1", Blocked: true}
	adapter, _, _, _, api, _ := newAdapter(botAccount(), conv, contact)

	_, err := adapter.SendText(context.Background(), entryContext(conv, nil),
		conversation.SendTextRequest{Body: "oi"})
	if !errors.Is(err, conversation.ErrOutboundWindowClosed) {
		t.Errorf("err = %v, want ErrOutboundWindowClosed", err)
	}
	if len(api.SentText) != 0 {
		t.Error("no API call should be made for a blocked contact")
	}
}

// 401 means the token was revoked in BotFather. Marking the account is what
// turns "messages silently stopped" into a visible Reconnect prompt.
func TestSendTextMarksAccountOnRevokedToken(t *testing.T) {
	conv := conversationFor(5041234567)
	contact := &tgdomain.Contact{ID: "contact-1"}
	account := botAccount()
	adapter, accounts, _, _, api, _ := newAdapter(account, conv, contact)

	api.SendTextFn = func(context.Context, string, tgdomain.SendTextInput) (*tgdomain.SendResult, error) {
		return nil, &tgdomain.APIError{Code: 401, Description: "Unauthorized"}
	}

	if _, err := adapter.SendText(context.Background(), entryContext(conv, nil),
		conversation.SendTextRequest{Body: "oi"}); err == nil {
		t.Fatal("expected an error")
	}

	if len(accounts.StatusWrites) != 1 || accounts.StatusWrites[0].Status != tgdomain.StatusTokenInvalid {
		t.Errorf("status writes = %+v, want a single TOKEN_INVALID", accounts.StatusWrites)
	}
}

// 403 means the customer blocked the bot. Flagging the contact is what disables
// the composer instead of letting every subsequent send fail identically.
func TestSendTextFlagsContactOnForbidden(t *testing.T) {
	conv := conversationFor(5041234567)
	contact := &tgdomain.Contact{ID: "contact-1"}
	adapter, _, contacts, _, api, _ := newAdapter(botAccount(), conv, contact)

	api.SendTextFn = func(context.Context, string, tgdomain.SendTextInput) (*tgdomain.SendResult, error) {
		return nil, &tgdomain.APIError{Code: 403, Description: "Forbidden: bot was blocked by the user"}
	}

	_, err := adapter.SendText(context.Background(), entryContext(conv, nil),
		conversation.SendTextRequest{Body: "oi"})
	if !errors.Is(err, conversation.ErrOutboundWindowClosed) {
		t.Errorf("err = %v, want the window-closed sentinel so the UI can explain it", err)
	}
	if len(contacts.BlockedWrites) != 1 || !contacts.BlockedWrites[0].Blocked {
		t.Errorf("blocked writes = %+v, want the contact flagged", contacts.BlockedWrites)
	}
}

// A group that became a supergroup has a NEW chat id. Rewriting both rows is what
// keeps the conversation reachable; not doing it kills it silently.
func TestSendTextRewritesMigratedChatID(t *testing.T) {
	conv := conversationFor(-1001111)
	contact := &tgdomain.Contact{ID: "contact-1"}
	adapter, _, contacts, conversations, api, _ := newAdapter(botAccount(), conv, contact)

	api.SendTextFn = func(context.Context, string, tgdomain.SendTextInput) (*tgdomain.SendResult, error) {
		return nil, &tgdomain.APIError{Code: 400, MigrateToChatID: -1002222}
	}

	if _, err := adapter.SendText(context.Background(), entryContext(conv, nil),
		conversation.SendTextRequest{Body: "oi"}); err == nil {
		t.Fatal("expected an error so the caller retries")
	}

	if len(conversations.ChatIDWrites) != 1 || conversations.ChatIDWrites[0] != -1002222 {
		t.Errorf("conversation chat id writes = %v, want the migrated id", conversations.ChatIDWrites)
	}
	if len(contacts.ChatIDWrites) != 1 || contacts.ChatIDWrites[0] != -1002222 {
		t.Errorf("contact chat id writes = %v, want the migrated id", contacts.ChatIDWrites)
	}
}

// A cached file_id has NO size limit and costs no upload. Preferring it is what
// makes a repeated boleto image free.
func TestSendMediaPrefersCachedFileID(t *testing.T) {
	conv := conversationFor(5041234567)
	contact := &tgdomain.Contact{ID: "contact-1"}
	adapter, _, _, _, api, files := newAdapter(botAccount(), conv, contact)

	const url = "https://cdn.example.com/boleto.jpg"

	// First send uploads by URL and caches the id Telegram assigned.
	if _, err := adapter.SendMedia(context.Background(), entryContext(conv, nil),
		conversation.SendMediaRequest{Kind: "image", URL: url, MIMEType: "image/jpeg"}); err != nil {
		t.Fatalf("SendMedia: %v", err)
	}
	if api.SentMedia[0].FileID != "" || api.SentMedia[0].URL != url {
		t.Error("the first send should go by URL")
	}
	if len(files.Puts) != 1 || files.Puts[0] != "cached-file-id" {
		t.Errorf("file id cache puts = %v, want the id Telegram returned", files.Puts)
	}

	// Second send reuses the cached id and sends no URL at all.
	if _, err := adapter.SendMedia(context.Background(), entryContext(conv, nil),
		conversation.SendMediaRequest{Kind: "image", URL: url, MIMEType: "image/jpeg"}); err != nil {
		t.Fatalf("SendMedia (second): %v", err)
	}
	second := api.SentMedia[1]
	if second.FileID != "cached-file-id" {
		t.Errorf("FileID = %q, want the cached id", second.FileID)
	}
	if second.URL != "" {
		t.Error("a cached send must not also carry a URL")
	}
}

// sendVoice renders an in-chat waveform but requires audio/ogg; anything else
// "will be sent as files". The MIME decides the method, not the caller.
func TestSendMediaPicksVoiceForOgg(t *testing.T) {
	conv := conversationFor(5041234567)
	contact := &tgdomain.Contact{ID: "contact-1"}
	adapter, _, _, _, api, _ := newAdapter(botAccount(), conv, contact)

	if _, err := adapter.SendMedia(context.Background(), entryContext(conv, nil),
		conversation.SendMediaRequest{Kind: "audio", URL: "https://x/a.ogg", MIMEType: "audio/ogg"}); err != nil {
		t.Fatalf("SendMedia: %v", err)
	}
	if api.SentMedia[0].Kind != tgdomain.MediaVoice {
		t.Errorf("kind = %q, want voice for audio/ogg", api.SentMedia[0].Kind)
	}

	if _, err := adapter.SendMedia(context.Background(), entryContext(conv, nil),
		conversation.SendMediaRequest{Kind: "audio", URL: "https://x/a.mp3", MIMEType: "audio/mpeg"}); err != nil {
		t.Fatalf("SendMedia: %v", err)
	}
	if api.SentMedia[1].Kind != tgdomain.MediaAudio {
		t.Errorf("kind = %q, want audio for mp3", api.SentMedia[1].Kind)
	}
}

// Documents accept any type on Telegram, unlike Instagram's PDF-only rule.
func TestSendMediaAcceptsAnyDocumentType(t *testing.T) {
	conv := conversationFor(5041234567)
	contact := &tgdomain.Contact{ID: "contact-1"}
	adapter, _, _, _, _, _ := newAdapter(botAccount(), conv, contact)

	for _, mime := range []string{"application/pdf", "text/csv", "application/zip"} {
		if _, err := adapter.SendMedia(context.Background(), entryContext(conv, nil),
			conversation.SendMediaRequest{Kind: "document", URL: "https://x/f", MIMEType: mime}); err != nil {
			t.Errorf("document %s rejected: %v", mime, err)
		}
	}
}

// MarkSeen is business-mode only. Returning "unsupported" rather than silently
// doing nothing is what stops a caller believing a read receipt was sent.
func TestMarkSeenIsBusinessOnly(t *testing.T) {
	conv := conversationFor(5041234567)
	contact := &tgdomain.Contact{ID: "contact-1"}

	adapter, _, _, _, _, _ := newAdapter(botAccount(), conv, contact)
	presence, ok := adapter.(conversation.PresenceAdapter)
	if !ok {
		t.Fatal("the Telegram adapter must implement PresenceAdapter")
	}

	err := presence.MarkSeen(context.Background(), entryContext(conv, nil),
		tgdomain.ProviderMessageID(77777, 5041234567, 1))
	if !errors.Is(err, conversation.ErrCapabilityUnsupported) {
		t.Errorf("err = %v, want ErrCapabilityUnsupported in bot mode", err)
	}
}

// Unsend is bounded by Telegram's own 48-hour rule. Enforcing it here names the
// reason instead of surfacing an opaque Bad Request to an operator undoing a
// mistake.
func TestRetractRefusesBeyond48Hours(t *testing.T) {
	conv := conversationFor(5041234567)
	contact := &tgdomain.Contact{ID: "contact-1"}
	adapter, _, _, _, _, _ := newAdapter(botAccount(), conv, contact)

	retracting, ok := adapter.(conversation.RetractingAdapter)
	if !ok {
		t.Fatal("the Telegram adapter must implement RetractingAdapter")
	}

	old := time.Now().UTC().Add(-49 * time.Hour)
	err := retracting.Retract(context.Background(), entryContext(conv, nil),
		tgdomain.ProviderMessageID(77777, 5041234567, 1), old)
	if !errors.Is(err, conversation.ErrCapabilityUnsupported) {
		t.Errorf("err = %v, want ErrCapabilityUnsupported past the 48h limit", err)
	}

	recent := time.Now().UTC().Add(-time.Hour)
	if err := retracting.Retract(context.Background(), entryContext(conv, nil),
		tgdomain.ProviderMessageID(77777, 5041234567, 1), recent); err != nil {
		t.Errorf("a one-hour-old message must be retractable: %v", err)
	}
}

// The adapter must implement editing, which is the capability only Telegram has.
func TestAdapterImplementsEditing(t *testing.T) {
	conv := conversationFor(5041234567)
	contact := &tgdomain.Contact{ID: "contact-1"}
	adapter, _, _, _, _, _ := newAdapter(botAccount(), conv, contact)

	editing, ok := adapter.(conversation.EditingAdapter)
	if !ok {
		t.Fatal("the Telegram adapter must implement EditingAdapter")
	}
	if err := editing.EditText(context.Background(), entryContext(conv, nil),
		tgdomain.ProviderMessageID(77777, 5041234567, 1), "corrigido"); err != nil {
		t.Errorf("EditText: %v", err)
	}
}

// A reply must leave from the SAME bot the message arrived on. With several bots
// in one workspace, the token bound to the call is the only thing that keeps
// them apart.
func TestSendUsesTheOwningAccountsToken(t *testing.T) {
	conv := conversationFor(5041234567)
	contact := &tgdomain.Contact{ID: "contact-1"}
	account := botAccount()
	account.BotToken = "88888:other-bot-secret"

	adapter, _, _, _, api, _ := newAdapter(account, conv, contact)
	if _, err := adapter.SendText(context.Background(), entryContext(conv, nil),
		conversation.SendTextRequest{Body: "oi"}); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	if api.SentTokens[0] != "88888:other-bot-secret" {
		t.Errorf("token = %q, want the owning account's", api.SentTokens[0])
	}
}

func TestEntryTypeIsTelegram(t *testing.T) {
	adapter, _, _, _, _, _ := newAdapter(botAccount(), conversationFor(1), &tgdomain.Contact{})
	if adapter.EntryType() != shared.EntryTypeTelegram {
		t.Errorf("EntryType = %q", adapter.EntryType())
	}
}
