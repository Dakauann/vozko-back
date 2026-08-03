package conversation_usecase

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

// The HTTP send endpoint accepts an {entryType} path variable, but its usecase
// was WhatsApp-only. Widening the handler's guard without widening the usecase
// turned a clear 400 into a 404 that LIED, the conversation exists and the
// WebSocket path can send on it perfectly well.
//
// These pin the two together.

type recordingSender struct {
	textCalls  []sentText
	mediaCalls []sentMedia
	err        error
}

type sentText struct {
	EntryID, EntryType, Text, UserID, ReplyTo string
}

type sentMedia struct {
	EntryID, EntryType, MediaID, MediaType, UserID, ReplyTo, Caption string
}

func (r *recordingSender) SendTextMessage(entryID, entryType, text, userID, replyTo string) (*conversation.Message, error) {
	r.textCalls = append(r.textCalls, sentText{entryID, entryType, text, userID, replyTo})
	if r.err != nil {
		return nil, r.err
	}
	return &conversation.Message{ID: "msg-1", EntryID: entryID, EntryType: shared.EntryType(entryType)}, nil
}

func (r *recordingSender) SendMediaMessage(entryID, entryType, mediaID, mediaType, userID, replyTo, caption string) (*conversation.Message, error) {
	r.mediaCalls = append(r.mediaCalls, sentMedia{entryID, entryType, mediaID, mediaType, userID, replyTo, caption})
	if r.err != nil {
		return nil, r.err
	}
	return &conversation.Message{ID: "msg-2", EntryID: entryID, EntryType: shared.EntryType(entryType)}, nil
}

// stubAdapter is enough to make a channel look registered; the usecase only asks
// the registry whether the entry type has one.
type stubAdapter struct{ entryType shared.EntryType }

func (s stubAdapter) EntryType() shared.EntryType { return s.entryType }
func (s stubAdapter) ResolveEntry(context.Context, string) (*conversation.EntryContext, error) {
	return nil, nil
}
func (s stubAdapter) WindowState(context.Context, *conversation.EntryContext) (bool, *time.Time, error) {
	return true, nil, nil
}
func (s stubAdapter) SendText(context.Context, *conversation.EntryContext, conversation.SendTextRequest) (*conversation.SendOutcome, error) {
	return nil, nil
}
func (s stubAdapter) SendMedia(context.Context, *conversation.EntryContext, conversation.SendMediaRequest) (*conversation.SendOutcome, error) {
	return nil, nil
}

func newSendUseCase(sender ChannelMessageSender, kinds ...shared.EntryType) *sendConversationMessageUseCase {
	adapters := make([]conversation.ChannelAdapter, 0, len(kinds))
	for _, k := range kinds {
		adapters = append(adapters, stubAdapter{entryType: k})
	}
	uc := &sendConversationMessageUseCase{}
	uc.SetChannelSender(conversation.NewAdapterRegistry(adapters...), sender)
	return uc
}

func TestSendRoutesAdapterBackedChannelsToTheSharedSender(t *testing.T) {
	for _, entryType := range []shared.EntryType{shared.EntryTypeTelegram, shared.EntryTypeInstagram} {
		t.Run(string(entryType), func(t *testing.T) {
			sender := &recordingSender{}
			uc := newSendUseCase(sender, shared.EntryTypeTelegram, shared.EntryTypeInstagram)

			msg, err := uc.Execute(conversation.SendMessageInput{
				EntryID:   "conv-1",
				EntryType: string(entryType),
				Text:      "oi",
				SenderID:  "user-1",
			})
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if msg == nil {
				t.Fatal("a successful send must return the message")
			}
			if len(sender.textCalls) != 1 {
				t.Fatalf("sender calls = %d, want 1", len(sender.textCalls))
			}
			// The entry type is passed through unchanged so the sender resolves the
			// right adapter, and therefore queries the right channel's table.
			if got := sender.textCalls[0].EntryType; got != string(entryType) {
				t.Errorf("entry type = %q, want %q", got, entryType)
			}
		})
	}
}

func TestSendRoutesMediaToTheSharedSender(t *testing.T) {
	sender := &recordingSender{}
	uc := newSendUseCase(sender, shared.EntryTypeTelegram)

	mediaID := "media-1"
	mediaType := conversation.MediaTypeImage
	if _, err := uc.Execute(conversation.SendMessageInput{
		EntryID:   "conv-1",
		EntryType: string(shared.EntryTypeTelegram),
		MediaID:   &mediaID,
		MediaType: &mediaType,
		Text:      "legenda",
		SenderID:  "user-1",
	}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if len(sender.mediaCalls) != 1 {
		t.Fatalf("media calls = %d, want 1", len(sender.mediaCalls))
	}
	call := sender.mediaCalls[0]
	if call.MediaID != mediaID || call.MediaType != string(mediaType) {
		t.Errorf("media = %q/%q, want %q/%q", call.MediaID, call.MediaType, mediaID, mediaType)
	}
	// Text becomes the caption on a media send rather than being dropped.
	if call.Caption != "legenda" {
		t.Errorf("caption = %q, want the text", call.Caption)
	}
	if len(sender.textCalls) != 0 {
		t.Error("a media send must not also send text")
	}
}

// A channel with neither an adapter nor the WhatsApp path must say so plainly.
// The previous behaviour reported ErrConversationNotFound, which sent an
// operator hunting for a conversation that was never missing.
func TestSendRejectsUnsupportedChannelHonestly(t *testing.T) {
	uc := newSendUseCase(&recordingSender{}, shared.EntryTypeTelegram)

	_, err := uc.Execute(conversation.SendMessageInput{
		EntryID:   "entry-1",
		EntryType: string(shared.EntryTypeSupport),
		Text:      "oi",
	})
	if !errors.Is(err, conversation.ErrEntryTypeInvalid) {
		t.Errorf("err = %v, want ErrEntryTypeInvalid", err)
	}
	if errors.Is(err, conversation.ErrConversationNotFound) {
		t.Error("an unsupported channel must not be reported as a missing conversation")
	}
}

// Without the sender wired, behaviour must be exactly what it was before: the
// WhatsApp path. This is what makes the change additive for existing tenants.
func TestSendWithoutAdaptersKeepsWhatsAppOnlyBehaviour(t *testing.T) {
	uc := &sendConversationMessageUseCase{}

	_, err := uc.Execute(conversation.SendMessageInput{
		EntryID:   "entry-1",
		EntryType: string(shared.EntryTypeTelegram),
		Text:      "oi",
	})
	if !errors.Is(err, conversation.ErrEntryTypeInvalid) {
		t.Errorf("err = %v, want ErrEntryTypeInvalid when no adapter is registered", err)
	}
}

// A send error from the shared path must surface, not be swallowed into a
// "conversation not found".
func TestSendPropagatesSenderErrors(t *testing.T) {
	sentinel := errors.New("window closed")
	sender := &recordingSender{err: sentinel}
	uc := newSendUseCase(sender, shared.EntryTypeTelegram)

	_, err := uc.Execute(conversation.SendMessageInput{
		EntryID:   "conv-1",
		EntryType: string(shared.EntryTypeTelegram),
		Text:      "oi",
	})
	if !errors.Is(err, sentinel) {
		t.Errorf("err = %v, want the sender's own error", err)
	}
}

func TestSendStillValidatesContent(t *testing.T) {
	uc := newSendUseCase(&recordingSender{}, shared.EntryTypeTelegram)

	if _, err := uc.Execute(conversation.SendMessageInput{
		EntryType: string(shared.EntryTypeTelegram), Text: "oi",
	}); !errors.Is(err, conversation.ErrConversationNotFound) {
		t.Errorf("missing entry id: err = %v", err)
	}

	if _, err := uc.Execute(conversation.SendMessageInput{
		EntryID: "conv-1", EntryType: string(shared.EntryTypeTelegram),
	}); err == nil {
		t.Error("a send with neither text nor media must be refused")
	}
}
