package node_executors

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"

	"vozko/domain/channel"
	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

type channelSender struct {
	whatsapp *whatsappSender
	adapters conversation.AdapterRegistry
	history  conversation.MessageHistoryManager
	media    conversation.ConversationMediaRepository
}

func newChannelSender(deps SenderDeps) *channelSender {
	return &channelSender{
		whatsapp: newWhatsAppSender(deps),
		adapters: deps.Adapters,
		history:  deps.HistoryManager,
		media:    deps.ConversationMediaRepo,
	}
}

type SentMessage struct {
	ProviderMessageID string
	AccountID         string
}

var ErrChannelCannotSend = workflow.ErrNodeConfigMissing

func (s *channelSender) SendText(
	ctx context.Context,
	run *workflow.WorkflowRun,
	text string,
	messageType conversation.MessageType,
) (*SentMessage, error) {
	if s == nil || run == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	entryType := shared.EntryType(run.EntryType)

	if entryType == shared.EntryTypeWhatsApp {
		if s.whatsapp == nil {
			return nil, nil
		}
		out, businessPhoneID, err := s.whatsapp.SendText(ctx, run, text, messageType)
		if err != nil || out == nil {
			return nil, err
		}
		return &SentMessage{ProviderMessageID: out.MessageID, AccountID: businessPhoneID}, nil
	}

	adapter := s.adapterFor(entryType)
	if adapter == nil {
		return nil, nil
	}

	ec, err := adapter.ResolveEntry(ctx, run.EntryID)
	if err != nil {
		return nil, err
	}

	window, err := adapter.WindowState(ctx, ec)
	if err != nil {
		return nil, err
	}
	if !window.Open {
		log.Printf("[workflow][run:%s] channel %s outbound window closed for entry=%s, message withheld",
			run.ID, entryType, run.EntryID)
		return nil, nil
	}

	outcome, err := adapter.SendText(ctx, ec, conversation.SendTextRequest{Body: text})
	if err != nil {
		return nil, err
	}

	providerID := ""
	if outcome != nil {
		providerID = outcome.ProviderMessageID
	}

	if s.history != nil {
		if err := s.history.Record(ctx, conversation.MessageDirectionOutbound, conversation.MessageHistoryRecord{
			EntryID:           run.EntryID,
			EntryType:         entryType,
			Channel:           conversation.MessageChannel(entryType),
			MessageType:       messageType,
			ProviderMessageID: providerID,
			From:              ec.AccountID,
			To:                ec.ContactRef,
			Text:              text,
			Timestamp:         time.Now().UTC(),
		}); err != nil {
			return nil, err
		}
	}

	return &SentMessage{ProviderMessageID: providerID, AccountID: ec.AccountID}, nil
}

func (s *channelSender) SendMedia(
	ctx context.Context,
	run *workflow.WorkflowRun,
	mediaURL, caption string,
) (*SentMessage, error) {
	if s == nil || run == nil || mediaURL == "" {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	entryType := shared.EntryType(run.EntryType)

	if entryType == shared.EntryTypeWhatsApp {
		if s.whatsapp == nil {
			return nil, nil
		}
		out, businessPhoneID, err := s.whatsapp.SendMedia(ctx, run, mediaURL, caption)
		if err != nil || out == nil {
			return nil, err
		}
		return &SentMessage{ProviderMessageID: out.MessageID, AccountID: businessPhoneID}, nil
	}

	adapter := s.adapterFor(entryType)
	if adapter == nil {
		return nil, nil
	}

	ec, err := adapter.ResolveEntry(ctx, run.EntryID)
	if err != nil {
		return nil, err
	}

	window, err := adapter.WindowState(ctx, ec)
	if err != nil {
		return nil, err
	}
	if !window.Open {
		log.Printf("[workflow][run:%s] channel %s outbound window closed for entry=%s, media withheld",
			run.ID, entryType, run.EntryID)
		return nil, nil
	}

	kind := detectMediaType(mediaURL)

	outcome, err := adapter.SendMedia(ctx, ec, conversation.SendMediaRequest{
		Kind:    kind,
		URL:     mediaURL,
		Caption: caption,
	})
	if err != nil {
		return nil, err
	}

	providerID := ""
	if outcome != nil {
		providerID = outcome.ProviderMessageID
	}

	if s.history != nil {
		mediaID, mediaKind := bridgeConversationMedia(s.media, run.EntryID, entryType, mediaURL, kind)

		if err := s.history.Record(ctx, conversation.MessageDirectionOutbound, conversation.MessageHistoryRecord{
			EntryID:           run.EntryID,
			EntryType:         entryType,
			Channel:           conversation.MessageChannel(entryType),
			MessageType:       conversation.MessageTypeMedia,
			ProviderMessageID: providerID,
			From:              ec.AccountID,
			To:                ec.ContactRef,
			MediaID:           mediaID,
			MediaType:         mediaKind,
			MediaURL:          mediaURL,
			Text:              caption,
			Timestamp:         time.Now().UTC(),
		}); err != nil {
			return nil, err
		}
	}

	return &SentMessage{ProviderMessageID: providerID, AccountID: ec.AccountID}, nil
}

func (s *channelSender) Supports(run *workflow.WorkflowRun) bool {
	if s == nil || run == nil {
		return false
	}
	if shared.EntryType(run.EntryType) == shared.EntryTypeWhatsApp {
		return s.whatsapp != nil
	}
	return s.adapterFor(shared.EntryType(run.EntryType)) != nil
}

func (s *channelSender) adapterFor(entryType shared.EntryType) conversation.ChannelAdapter {
	if s.adapters == nil {
		return nil
	}
	adapter, err := s.adapters.For(entryType)
	if err != nil {
		return nil
	}
	return adapter
}

func skipUnsupportedNode(ctx *workflow.NodeContext, nodeKind string) *workflow.NodeResult {
	log.Printf("[workflow][node:%s][run:%s] %s is not supported on channel %q, node skipped, run continues (entry=%s)",
		ctx.Node.ID, ctx.Run.ID, nodeKind, ctx.Run.EntryType, ctx.Run.EntryID)
	return &workflow.NodeResult{
		Output: map[string]interface{}{
			"sent":              false,
			"skipped":           true,
			"skipped_reason":    "unsupported_on_channel",
			"skipped_channel":   ctx.Run.EntryType,
			"skipped_node_kind": nodeKind,
		},
	}
}

func newChannelSenderFromWhatsApp(wa *whatsappSender) *channelSender {
	s := &channelSender{whatsapp: wa}
	if wa != nil {
		s.adapters = wa.deps.Adapters
		s.history = wa.deps.HistoryManager
		s.media = wa.deps.ConversationMediaRepo
	}
	return s
}

func (s *channelSender) SendInteractive(
	ctx context.Context,
	run *workflow.WorkflowRun,
	req conversation.SendInteractiveRequest,
) (*SentMessage, error) {
	if s == nil || run == nil || len(req.Options) == 0 {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	adapter, base := s.interactiveAdapterFor(shared.EntryType(run.EntryType))
	if adapter == nil {
		return nil, nil
	}

	ec, err := base.ResolveEntry(ctx, run.EntryID)
	if err != nil {
		return nil, err
	}

	window, err := base.WindowState(ctx, ec)
	if err != nil {
		return nil, err
	}
	if !window.Open {
		log.Printf("[workflow][run:%s] channel %s outbound window closed for entry=%s, prompt withheld",
			run.ID, run.EntryType, run.EntryID)
		return nil, nil
	}

	outcome, err := adapter.SendInteractive(ctx, ec, req)
	if err != nil {
		return nil, err
	}

	providerID := ""
	if outcome != nil {
		providerID = outcome.ProviderMessageID
	}

	if s.history != nil {
		if err := s.history.Record(ctx, conversation.MessageDirectionOutbound, conversation.MessageHistoryRecord{
			EntryID:           run.EntryID,
			EntryType:         shared.EntryType(run.EntryType),
			Channel:           conversation.MessageChannel(run.EntryType),
			MessageType:       conversation.MessageTypeAIResponse,
			ProviderMessageID: providerID,
			From:              ec.AccountID,
			To:                ec.ContactRef,
			Text:              req.Body,
			Timestamp:         time.Now().UTC(),
		}); err != nil {
			return nil, err
		}
	}

	return &SentMessage{ProviderMessageID: providerID, AccountID: ec.AccountID}, nil
}

func (s *channelSender) SupportsInteractive(run *workflow.WorkflowRun) bool {
	if s == nil || run == nil {
		return false
	}
	if shared.EntryType(run.EntryType) == shared.EntryTypeWhatsApp {
		return s.whatsapp != nil
	}
	adapter, _ := s.interactiveAdapterFor(shared.EntryType(run.EntryType))
	return adapter != nil
}

func (s *channelSender) interactiveAdapterFor(entryType shared.EntryType) (conversation.InteractiveAdapter, conversation.ChannelAdapter) {
	base := s.adapterFor(entryType)
	if base == nil {
		return nil, nil
	}
	interactive, ok := base.(conversation.InteractiveAdapter)
	if !ok {
		return nil, nil
	}
	return interactive, base
}

func (s *channelSender) InteractiveSupport() map[shared.EntryType]channel.InteractiveLimits {
	out := make(map[shared.EntryType]channel.InteractiveLimits, 4)
	if s != nil && s.whatsapp != nil {
		out[shared.EntryTypeWhatsApp] = conversation.WhatsAppInteractiveLimits()
	}
	if s == nil || s.adapters == nil {
		return out
	}
	for _, entryType := range s.adapters.EntryTypes() {
		if interactive, _ := s.interactiveAdapterFor(entryType); interactive != nil {
			out[entryType] = interactive.InteractiveLimits()
		}
	}
	return out
}

func (s *channelSender) SendSegments(
	ctx context.Context,
	run *workflow.WorkflowRun,
	segments []string,
	pause func(time.Duration),
) (bool, error) {
	if s == nil || run == nil || len(segments) == 0 {
		return false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if pause == nil {
		pause = time.Sleep
	}

	adapter := s.adapterFor(shared.EntryType(run.EntryType))
	if adapter == nil {
		return false, nil
	}

	ec, err := adapter.ResolveEntry(ctx, run.EntryID)
	if err != nil {
		return false, err
	}
	window, err := adapter.WindowState(ctx, ec)
	if err != nil {
		return false, err
	}
	if !window.Open {
		log.Printf("[workflow][run:%s] channel %s outbound window closed, %d segment(s) withheld",
			run.ID, run.EntryType, len(segments))
		return false, nil
	}

	presence, _ := adapter.(conversation.PresenceAdapter)

	for i, text := range segments {
		if presence != nil {
			if err := presence.SendTyping(ctx, ec, true); err != nil {
				log.Printf("[workflow][run:%s] typing indicator failed on segment %d: %v", run.ID, i+1, err)
			}
		}
		pause(segmentedTypingMinShow)

		outcome, err := adapter.SendText(ctx, ec, conversation.SendTextRequest{Body: text})
		if err != nil {
			log.Printf("[workflow][run:%s] segment %d/%d failed: %v", run.ID, i+1, len(segments), err)
			return false, err
		}

		providerID := ""
		if outcome != nil {
			providerID = outcome.ProviderMessageID
		}
		if s.history != nil {
			if err := s.history.Record(ctx, conversation.MessageDirectionOutbound, conversation.MessageHistoryRecord{
				EntryID:           run.EntryID,
				EntryType:         shared.EntryType(run.EntryType),
				Channel:           conversation.MessageChannel(run.EntryType),
				MessageType:       conversation.MessageTypeAIResponse,
				ProviderMessageID: providerID,
				From:              ec.AccountID,
				To:                ec.ContactRef,
				Text:              text,
				Timestamp:         time.Now().UTC(),
			}); err != nil {
				return false, err
			}
		}
		log.Printf("[workflow][run:%s] segment %d/%d sent on %s id=%s",
			run.ID, i+1, len(segments), run.EntryType, providerID)

		if i < len(segments)-1 {
			pause(segmentedTypingDelay)
		}
	}
	return true, nil
}

func bridgeConversationMedia(
	repo conversation.ConversationMediaRepository,
	entryID string,
	entryType shared.EntryType,
	mediaURL, mediaType string,
) (string, conversation.MediaType) {
	kind := conversation.MediaType(mediaType)
	if repo == nil || entryID == "" || mediaURL == "" || !kind.Valid() {
		return "", ""
	}

	media := &conversation.ConversationMedia{
		ID:               uuid.NewString(),
		EntryID:          entryID,
		EntryType:        entryType,
		Type:             kind,
		URL:              mediaURL,
		OriginalFilename: mediaFilename(mediaURL),
		CreatedAt:        time.Now().UTC(),
	}
	media.Normalize()
	if err := media.Validate(); err != nil {
		log.Printf("[workflow][media-bridge] invalid conversation media for entry %s: %v", entryID, err)
		return "", ""
	}
	if err := repo.Create(media); err != nil {
		log.Printf("[workflow][media-bridge] could not register media for entry %s: %v", entryID, err)
		return "", ""
	}
	return media.ID, kind
}
