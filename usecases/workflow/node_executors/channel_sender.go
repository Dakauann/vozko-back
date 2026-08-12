package node_executors

import (
	"context"
	"log"
	"time"

	"vozko/domain/channel"
	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

// channelSender routes a workflow's outbound message to whichever channel the
// run belongs to.
//
// WhatsApp keeps its dedicated sender: it resolves a lead number, picks between
// the campaign phone and the phone the customer wrote to, checks the 24h lead
// window and consumes template balance, none of which generalizes.
//
// Every other channel goes through the shared ChannelAdapter registry, which
// already owns that channel's send call and its outbound-window rule. So a
// channel that can be replied to from the CRM can also be replied to from a
// workflow, with no workflow code per channel.
type channelSender struct {
	whatsapp *whatsappSender
	adapters conversation.AdapterRegistry
	history  conversation.MessageHistoryManager
}

func newChannelSender(deps SenderDeps) *channelSender {
	return &channelSender{
		whatsapp: newWhatsAppSender(deps),
		adapters: deps.Adapters,
		history:  deps.HistoryManager,
	}
}

// SentMessage is the channel-neutral result of a workflow send.
type SentMessage struct {
	// ProviderMessageID is the id the channel assigned, when it reports one.
	ProviderMessageID string
	// AccountID is the channel account the message left from (a WhatsApp
	// business phone id, an Instagram account id). Surfaced for observability.
	AccountID string
}

// ErrChannelCannotSend is returned when the run's channel has no send path at
// all, neither the WhatsApp sender nor a registered adapter.
var ErrChannelCannotSend = workflow.ErrNodeConfigMissing

// SendText delivers text on the run's channel.
//
// A nil result with a nil error means the channel declined to send for a reason
// that is not a fault, most often a closed outbound window, which on Instagram
// is normal and only the contact can reopen.
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

	// The window is the channel's own rule, read from the same adapter the CRM
	// composer reads, so a workflow can never send where an operator could not.
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

	// Persist through the shared history manager so the transcript, dedup and
	// websocket fan-out match every other message on this channel.
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

// SendMedia delivers an attachment on the run's channel.
//
// It mirrors SendText exactly, including the window check, because the rule
// "a workflow can never send where an operator could not" does not change with
// the payload. The media node used to refuse every non-WhatsApp channel, so a
// workflow that answered a Telegram contact in text went silent the moment it
// reached an image.
//
// Returning (nil, nil) means the channel declined for a non-fault reason,
// a closed window, or no adapter, matching SendText.
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
		if err := s.history.Record(ctx, conversation.MessageDirectionOutbound, conversation.MessageHistoryRecord{
			EntryID:           run.EntryID,
			EntryType:         entryType,
			Channel:           conversation.MessageChannel(entryType),
			MessageType:       conversation.MessageTypeMedia,
			ProviderMessageID: providerID,
			From:              ec.AccountID,
			To:                ec.ContactRef,
			// Caption as the text, and no MediaID: the workflow's media id comes
			// from the media library, not the conversation-media space the
			// transcript renders from. This mirrors the WhatsApp media node
			// exactly, the attachment reaches the customer, and the transcript
			// records that a media message was sent. Rendering the thumbnail in
			// the transcript needs a media-id bridge that no channel has today.
			Text:      caption,
			Timestamp: time.Now().UTC(),
		}); err != nil {
			return nil, err
		}
	}

	return &SentMessage{ProviderMessageID: providerID, AccountID: ec.AccountID}, nil
}

// Supports reports whether the run's channel can send at all. Nodes use it to
// decide between sending and skipping.
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

// skipUnsupportedNode records that a node could not run on this run's channel.
//
// The run continues by product decision, so the log line is the only trace an
// operator gets, it names the node, the run and the channel precisely, because
// "the workflow completed but the customer got nothing" is otherwise very hard
// to reconstruct.
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

// newChannelSenderFromWhatsApp adapts an already-built WhatsApp sender into a
// channel sender. Used where a constructor receives the WhatsApp sender directly
// and its adapter registry arrives through the same deps.
func newChannelSenderFromWhatsApp(wa *whatsappSender) *channelSender {
	s := &channelSender{whatsapp: wa}
	if wa != nil {
		s.adapters = wa.deps.Adapters
		s.history = wa.deps.HistoryManager
	}
	return s
}

// SendInteractive delivers a single-choice prompt on the run's channel.
//
// Unlike SendText and SendMedia, this one is NOT implemented by every adapter:
// presenting choices is an optional capability, so the adapter is type-asserted
// and a channel without it is reported as unsupported rather than silently sent
// a wall of text listing options the contact cannot tap.
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
			// The transcript stores the prompt body. The options themselves are
			// rendered by the provider and are not part of the message text on
			// any of these channels.
			Text:      req.Body,
			Timestamp: time.Now().UTC(),
		}); err != nil {
			return nil, err
		}
	}

	return &SentMessage{ProviderMessageID: providerID, AccountID: ec.AccountID}, nil
}

// SupportsInteractive reports whether the run's channel can ask the contact to
// pick an option. WhatsApp qualifies through its own sender.
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

// interactiveAdapterFor returns the interactive capability and the base adapter
// it belongs to, or (nil, nil) when the channel cannot present choices.
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

// InteractiveSupport reports every channel's option limits, for the workflow
// editor.
//
// Built by walking the adapter registry rather than from a hardcoded list, so a
// channel added later appears here the moment its adapter is registered.
// WhatsApp is added explicitly because it has no adapter yet, it is the
// channel still being migrated onto the abstraction.
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

// SendSegments delivers a reply as several paced messages on an adapter-backed
// channel.
//
// Segmented mode exists so a long answer arrives the way a person types it
// rather than as one wall of text. It was implemented only for WhatsApp, and
// the default single-send path was gated on NOT being segmented, so on every
// other channel a segmented agent generated a reply, billed for it, and sent
// nothing at all. Silent, and invisible in the run's own logs.
//
// The typing indicator is best-effort through the optional PresenceAdapter:
// channels that have one look natural, channels that do not still get the
// pacing. Returns true only when every segment was delivered.
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
			// Typing before each segment, not only between them: the first pause
			// is what makes the opening message feel composed rather than instant.
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
