package conversation_usecase

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"

	"vozko/domain/conversation"
	"vozko/domain/media"
	"vozko/domain/shared"
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

	// MediaID names a row in the workspace MEDIA LIBRARY (`medias`), not in
	// conversation media. A campaign attaches one curated file to thousands of
	// conversations, so the library is the only store the id can come from —
	// which is also why it needs WorkspaceID to be checked against.
	MediaID   string
	MediaType string
	// FileName is what a document renders as on the contact's device. The
	// library row does not carry the operator's original filename; the campaign
	// spec does.
	FileName string

	// WorkspaceID owns the campaign, and is what a library media id is
	// authorised against.
	WorkspaceID string

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
		// The LIBRARY, not conversation media.
		//
		// This used to read s.mediaRepo, which is the conversation media store:
		// rows scoped to one entry, written when an operator uploads into a
		// chat or when an inbound attachment arrives. A campaign's id never
		// exists there, so every media campaign failed on its first recipient
		// with "campaign send: media not found: conversation: media not found"
		// and no attachment was ever delivered on this channel.
		libraryMedia, err := s.lookupCampaignMedia(in.WorkspaceID, in.MediaID)
		if err != nil {
			return nil, err
		}

		// The transcript resolves conversation_messages.media_id against
		// conversation media, so the library id may not be written onto the
		// message: it would point the CRM at a row that does not exist and the
		// contact would receive a file the operator cannot see. Registering the
		// delivered file per conversation is what the workflow send does too
		// (bridgeConversationMedia).
		//
		// Registered BEFORE the send because sendViaAdapter takes the id it
		// records up front. A row whose send then fails is inert: no message
		// references it, and nothing lists conversation media on its own.
		transcriptMediaID := s.registerCampaignMediaInConversation(in, libraryMedia.URL)

		return s.sendViaAdapter(adapter, in.EntryID, in.EntryType, "", "",
			func(ec *conversation.EntryContext) (*conversation.SendOutcome, error) {
				return adapter.SendMedia(context.Background(), ec, conversation.SendMediaRequest{
					// Never human-initiated. The zero value already means
					// automated, but saying it here is what documents that a
					// campaign is precisely the traffic the pacing exists for.
					HumanInitiated: false,
					Kind:           mediaKindForChannel(in.MediaType),
					URL:            libraryMedia.URL,
					// No MIMEType: a library row does not store one, and the
					// adapters treat an empty type as "unknown, do not refuse"
					// rather than guessing one that could be wrong.
					FileName: in.FileName,
					Caption:  in.Text,
				})
			},
			conversation.MessageTypeMedia, in.Text, transcriptMediaID, in.MediaType)

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

// lookupCampaignMedia resolves the campaign's attachment from the workspace
// media library.
//
// Scoped to the campaign's own workspace: the create endpoint takes a media id
// from the client and never validates it, so without this check a campaign
// could name another workspace's file and blast it to its own contacts. A
// mismatch answers exactly like a missing row, so the lookup cannot be used to
// probe for ids across workspaces — the same rule domain/media states.
func (s *MessageSenderService) lookupCampaignMedia(workspaceID, mediaID string) (*media.Media, error) {
	if s.mediaLibrary == nil {
		return nil, fmt.Errorf("campaign send: media library is not configured")
	}
	record, err := s.mediaLibrary.GetMediaByID(mediaID)
	if err != nil {
		return nil, fmt.Errorf("campaign send: media not found: %w", err)
	}
	if record == nil || record.URL == "" {
		return nil, fmt.Errorf("campaign send: %w", media.ErrMediaNotFound)
	}
	if workspaceID != "" && record.WorkspaceID != workspaceID {
		return nil, fmt.Errorf("campaign send: %w", media.ErrMediaNotFound)
	}
	return record, nil
}

// registerCampaignMediaInConversation gives the transcript a row to render the
// delivered attachment from, and answers "" when it cannot.
//
// Best effort on purpose: an empty id costs the transcript its thumbnail, while
// returning an error here would fail a recipient the campaign could otherwise
// reach — and on a blast that turns one bookkeeping fault into thousands of
// undelivered messages.
func (s *MessageSenderService) registerCampaignMediaInConversation(in SendCampaignMessageInput, url string) string {
	kind := conversation.MediaType(in.MediaType)
	if s.mediaRepo == nil || url == "" || !kind.Valid() {
		return ""
	}

	record := &conversation.ConversationMedia{
		ID:               uuid.NewString(),
		EntryID:          in.EntryID,
		EntryType:        shared.EntryType(in.EntryType),
		Type:             kind,
		URL:              url,
		OriginalFilename: in.FileName,
		CreatedAt:        time.Now().UTC(),
	}
	record.Normalize()
	if err := record.Validate(); err != nil {
		log.Printf("[campaign-send] invalid conversation media for entry %s: %v", in.EntryID, err)
		return ""
	}
	if err := s.mediaRepo.Create(record); err != nil {
		log.Printf("[campaign-send] could not register media for entry %s: %v", in.EntryID, err)
		return ""
	}
	return record.ID
}
