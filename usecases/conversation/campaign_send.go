package conversation_usecase

import (
	"context"
	"fmt"

	"vozko/domain/conversation"
)

// The campaign send path.
//
// A campaign's message must be the CRM's message, delivered by the SAME channel
// adapter the composer uses. Anything else and a blast would bypass five
// behaviours the composer already gets right: the channel's pacing, its
// restriction caching, its placeholder neutralisation, its media limits, and the
// transcript itself.
//
// So this is not a second sender. It is a third caller of sendViaAdapter,
// alongside the operator and the AI, differing only in who it is attributed to
// and in the fact that it is never human-initiated.

// SendCampaignMessageInput is one campaign message to one conversation.
//
// Exactly one of Text, MediaID or Options describes the payload; the domain's
// MessageSpec has already validated which.
type SendCampaignMessageInput struct {
	// EntryID is the conversation, EntryType its channel.
	EntryID   string
	EntryType string

	// Text is the rendered body. For a media send it is the caption; for a menu
	// it is the prompt.
	Text string

	// MediaID references our media store. The adapter resolves it to the URL the
	// provider fetches.
	MediaID   string
	MediaType string

	// Options make this an interactive prompt.
	Options []conversation.InteractiveOption
	Style   string
	Footer  string
}

// SendCampaignMessage delivers one campaign message and records it.
//
// Attribution is empty rather than a user id: nobody typed this. The message
// lands in the transcript as an outbound message with no sender, which is what
// the CRM renders for automation, and is why a campaign send appears in the
// conversation exactly as a real message does — because it is one.
func (s *MessageSenderService) SendCampaignMessage(in SendCampaignMessageInput) (*conversation.Message, error) {
	adapter := s.adapterFor(in.EntryType)
	if adapter == nil {
		// A channel with no adapter has no campaign path. Refusing here beats
		// falling through to the WhatsApp Cloud pipeline below, which would
		// charge template balance for a message this channel never bills.
		return nil, conversation.ErrNoAdapterForEntryType
	}

	switch {
	case len(in.Options) > 0:
		interactive, ok := adapter.(conversation.InteractiveAdapter)
		if !ok {
			return nil, fmt.Errorf("campaign send: %s cannot deliver interactive prompts", in.EntryType)
		}
		return s.sendViaAdapter(adapter, in.EntryID, in.EntryType, "", "",
			func(ec *conversation.EntryContext) (*conversation.SendOutcome, error) {
				return interactive.SendInteractive(context.Background(), ec, conversation.SendInteractiveRequest{
					Body:    in.Text,
					Footer:  in.Footer,
					Options: in.Options,
					Style:   in.Style,
				})
			},
			conversation.MessageTypeOperator, in.Text, "", "")

	case in.MediaID != "":
		mediaRecord, err := s.mediaRepo.GetByID(in.MediaID)
		if err != nil || mediaRecord == nil {
			return nil, fmt.Errorf("campaign send: media not found: %w", err)
		}
		return s.sendViaAdapter(adapter, in.EntryID, in.EntryType, "", "",
			func(ec *conversation.EntryContext) (*conversation.SendOutcome, error) {
				return adapter.SendMedia(context.Background(), ec, conversation.SendMediaRequest{
					// Never human-initiated. The zero value already means
					// automated, but saying it here is what documents that a
					// campaign is precisely the traffic the pacing exists for.
					HumanInitiated: false,
					Kind:           mediaKindForChannel(in.MediaType),
					URL:            mediaRecord.URL,
					MIMEType:       mediaRecord.MimeType,
					FileName:       mediaRecord.OriginalFilename,
					Caption:        in.Text,
				})
			},
			conversation.MessageTypeMedia, in.Text, in.MediaID, in.MediaType)

	default:
		return s.sendViaAdapter(adapter, in.EntryID, in.EntryType, "", "",
			func(ec *conversation.EntryContext) (*conversation.SendOutcome, error) {
				return adapter.SendText(context.Background(), ec, conversation.SendTextRequest{
					HumanInitiated: false,
					Body:           in.Text,
				})
			},
			conversation.MessageTypeOperator, in.Text, "", "")
	}
}
