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

func TestWindowStateBotModeHasNoClock(t *testing.T) {
	conv := conversationFor(5041234567)
	contact := &tgdomain.Contact{ID: "contact-1", TGUserID: 5041234567}
	adapter, _, _, _, _, _ := newAdapter(botAccount(), conv, contact)

	old := time.Now().UTC().Add(-7 * 24 * time.Hour)
	window, err := adapter.WindowState(context.Background(), entryContext(conv, &old))
	if err != nil {
		t.Fatalf("WindowState: %v", err)
	}
	if !window.Open {
		t.Error("bot mode has no window; a week-old conversation is still repliable")
	}
	if window.ExpiresAt != nil {
		t.Errorf("expiry = %v, want nil, there is no moment at which bot mode reopens", window.ExpiresAt)
	}
}

func TestWindowStateBotModeClosesWhenBlocked(t *testing.T) {
	conv := conversationFor(5041234567)
	contact := &tgdomain.Contact{ID: "contact-1", TGUserID: 5041234567, Blocked: true}
	adapter, _, _, _, _, _ := newAdapter(botAccount(), conv, contact)

	window, err := adapter.WindowState(context.Background(), entryContext(conv, nil))
	if err != nil {
		t.Fatalf("WindowState: %v", err)
	}
	if window.Open {
		t.Error("a blocked contact closes the composer")
	}
	if window.ExpiresAt != nil {
		t.Error("being blocked is not a clock; there is no expiry to show")
	}
}

func TestWindowStateBusinessModeUsesTheClock(t *testing.T) {
	conv := conversationFor(5041234567)
	contact := &tgdomain.Contact{ID: "contact-1", TGUserID: 5041234567}
	adapter, _, _, _, _, _ := newAdapter(businessAccount(true), conv, contact)

	fresh := time.Now().UTC().Add(-time.Hour)
	window, err := adapter.WindowState(context.Background(), entryContext(conv, &fresh))
	if err != nil {
		t.Fatalf("WindowState: %v", err)
	}
	if !window.Open || window.ExpiresAt == nil {
		t.Fatal("a recent inbound leaves the business window open, with an expiry to show")
	}

	stale := time.Now().UTC().Add(-25 * time.Hour)
	if stale, _ := adapter.WindowState(context.Background(), entryContext(conv, &stale)); stale.Open {
		t.Error("a 25-hour-old inbound closes the business window")
	}
}

func TestWindowStateBusinessModeWithoutCanReply(t *testing.T) {
	conv := conversationFor(5041234567)
	contact := &tgdomain.Contact{ID: "contact-1"}
	adapter, _, _, _, _, _ := newAdapter(businessAccount(false), conv, contact)

	fresh := time.Now().UTC().Add(-time.Minute)
	window, err := adapter.WindowState(context.Background(), entryContext(conv, &fresh))
	if err != nil {
		t.Fatalf("WindowState: %v", err)
	}
	if window.Open {
		t.Error("without can_reply the composer is closed even inside the 24h window")
	}
	if window.ExpiresAt != nil {
		t.Error("a revoked right has no expiry")
	}
}

func TestSendTextEnforcesRuneLimit(t *testing.T) {
	conv := conversationFor(5041234567)
	contact := &tgdomain.Contact{ID: "contact-1"}
	adapter, _, _, _, api, _ := newAdapter(botAccount(), conv, contact)

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

func TestSendMediaPrefersCachedFileID(t *testing.T) {
	conv := conversationFor(5041234567)
	contact := &tgdomain.Contact{ID: "contact-1"}
	adapter, _, _, _, api, files := newAdapter(botAccount(), conv, contact)

	const url = "https://cdn.example.com/boleto.jpg"

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
