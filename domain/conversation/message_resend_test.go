package conversation

import (
	"testing"
	"time"

	"vozko/domain/shared"
)

func failedOperatorMessage() *Message {
	return &Message{
		ID: "m1", EntryID: "entry-1", EntryType: shared.EntryTypeWhatsApp,
		MessageType: MessageTypeOperator, Text: "oi", SentBy: SentByPerson("user-ana"),
		DeliveryStatus: DeliveryStatusFailed,
	}
}

func TestCheckResendOnlyAcceptsAFailedOperatorMessageOfThatConversation(t *testing.T) {
	cases := map[string]struct {
		change func(*Message)
		want   error
	}{
		"a failed operator message":         {func(*Message) {}, nil},
		"a message of another conversation": {func(m *Message) { m.EntryID = "entry-2" }, ErrMessageNotFound},
		"a message of another channel":      {func(m *Message) { m.EntryType = shared.EntryTypeTelegram }, ErrMessageNotFound},
		"a delivered message":               {func(m *Message) { m.DeliveryStatus = DeliveryStatusDelivered }, ErrResendNotAllowed},
		"a message the customer sent":       {func(m *Message) { m.SentBy = SentByContact("5511") }, ErrResendNotAllowed},
		"a failed AI reply":                 {func(m *Message) { m.SentBy = SentByAI("agent-1") }, ErrResendNotAllowed},
		"a message that was already resent": {func(m *Message) { _ = m.MarkResentAs("m2") }, ErrResendNotAllowed},
		"a message that was deleted":        {func(m *Message) { m.Retract(time.Now()) }, ErrResendNotAllowed},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			m := failedOperatorMessage()
			tc.change(m)
			if got := CheckResend(m, "entry-1", shared.EntryTypeWhatsApp); got != tc.want {
				t.Fatalf("CheckResend = %v, want %v", got, tc.want)
			}
		})
	}
	if got := CheckResend(nil, "entry-1", shared.EntryTypeWhatsApp); got != ErrMessageNotFound {
		t.Fatalf("CheckResend(nil) = %v, want ErrMessageNotFound", got)
	}
}

func TestCheckResendOnlyAcceptsKindsTheOperatorSendReproducesExactly(t *testing.T) {
	media := func(kind MediaType) func(*Message) {
		return func(m *Message) {
			id := "media-1"
			m.MediaID, m.MediaType, m.Text = &id, kind, "legenda"
		}
	}
	cases := map[string]struct {
		change func(*Message)
		want   error
	}{
		"plain text":                  {func(*Message) {}, nil},
		"an image with its caption":   {media(MediaTypeImage), nil},
		"a video with its caption":    {media(MediaTypeVideo), nil},
		"a document with its caption": {media(MediaTypeDocument), nil},
		"a voice note":                {media(MediaTypeAudio), nil},
		"a button message with its buttons": {func(m *Message) {
			_ = m.AttachButtons(SendButtonInput{BodyText: "escolha", Buttons: []ButtonPayload{{ID: "b1", Title: "Sim"}}})
		}, nil},
		"a template would lose its structure": {func(m *Message) { m.MessageType = MessageTypeTemplate }, ErrResendNotAllowed},
		"a sticker":                           {media(MediaTypeSticker), ErrResendNotAllowed},
		"media of an unknown kind":            {media(MediaType("")), ErrResendNotAllowed},
		"a media kind without its file":       {func(m *Message) { m.MediaType = MediaTypeImage }, ErrResendNotAllowed},
		"inline image bytes":                  {func(m *Message) { m.Image = []byte{1} }, ErrResendNotAllowed},
		"a shared location":                   {func(m *Message) { m.Metadata = []byte(`{"location":{"latitude":-23.5,"longitude":-46.6}}`) }, ErrResendNotAllowed},
		"a location written as text": {func(m *Message) {
			m.Channel, m.Text = MessageChannelWhatsApp, "📍 Casa, Rua A (-23.500000, -46.600000)"
		}, ErrResendNotAllowed},
		"a reaction":        {func(m *Message) { m.MessageType = MessageTypeReaction }, ErrResendNotAllowed},
		"a sticker message": {func(m *Message) { m.MessageType = MessageTypeSticker }, ErrResendNotAllowed},
		"a call event":      {func(m *Message) { m.MessageType = MessageTypeCallMissed }, ErrResendNotAllowed},
		"an empty text":     {func(m *Message) { m.Text = "  " }, ErrResendNotAllowed},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			m := failedOperatorMessage()
			tc.change(m)
			if got := CheckResend(m, "entry-1", shared.EntryTypeWhatsApp); got != tc.want {
				t.Fatalf("CheckResend = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestResendInputCarriesTheOriginalContent(t *testing.T) {
	replyTo, media := "m0", "media-1"

	text := failedOperatorMessage()
	text.ReplyToMessageID = &replyTo
	in := text.ResendInput()
	if in.EntryID != "entry-1" || in.EntryType != "whatsapp" || in.Text != "oi" || in.ReplyToMessageID != "m0" || in.MediaID != "" || in.Buttons != nil {
		t.Fatalf("text resend = %+v", in)
	}

	withMedia := failedOperatorMessage()
	withMedia.MediaID, withMedia.MediaType, withMedia.Text = &media, MediaTypeImage, "legenda"
	in = withMedia.ResendInput()
	if in.MediaID != "media-1" || in.MediaType != "image" || in.Text != "legenda" {
		t.Fatalf("media resend = %+v", in)
	}

	withButtons := failedOperatorMessage()
	if err := withButtons.AttachButtons(SendButtonInput{BodyText: "escolha", Buttons: []ButtonPayload{{ID: "b1", Title: "Sim"}}}); err != nil {
		t.Fatal(err)
	}
	in = withButtons.ResendInput()
	if in.Buttons == nil || in.Buttons.BodyText != "escolha" || in.Text != "" {
		t.Fatalf("buttons resend = %+v", in)
	}
}
