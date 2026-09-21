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

type SendCampaignMessageInput struct {
	EntryID   string
	EntryType string

	Text string

	MediaID   string
	MediaType string
	FileName  string

	WorkspaceID string

	Options []conversation.InteractiveOption
	Style   string
	Footer  string
}

func (s *MessageSenderService) SendCampaignMessage(in SendCampaignMessageInput) (*conversation.Message, error) {
	adapter := s.adapterFor(in.EntryType)
	if adapter == nil {
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
		libraryMedia, err := s.lookupCampaignMedia(in.WorkspaceID, in.MediaID)
		if err != nil {
			return nil, err
		}

		transcriptMediaID := s.registerCampaignMediaInConversation(in, libraryMedia.URL)

		return s.sendViaAdapter(adapter, in.EntryID, in.EntryType, "", "",
			func(ec *conversation.EntryContext) (*conversation.SendOutcome, error) {
				return adapter.SendMedia(context.Background(), ec, conversation.SendMediaRequest{
					HumanInitiated: false,
					Kind:           mediaKindForChannel(in.MediaType),
					URL:            libraryMedia.URL,
					FileName:       in.FileName,
					Caption:        in.Text,
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
