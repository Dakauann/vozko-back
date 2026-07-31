package node_executors

import (
	"context"
	"log"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

// channelSender routes a workflow's outbound message to whichever channel the
// run belongs to.
//
// WhatsApp keeps its dedicated sender: it resolves a lead number, picks between
// the campaign phone and the phone the customer wrote to, checks the 24h lead
// window and consumes template balance — none of which generalizes.
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
// all — neither the WhatsApp sender nor a registered adapter.
var ErrChannelCannotSend = workflow.ErrNodeConfigMissing

// SendText delivers text on the run's channel.
//
// A nil result with a nil error means the channel declined to send for a reason
// that is not a fault — most often a closed outbound window, which on Instagram
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
	open, _, err := adapter.WindowState(ctx, ec)
	if err != nil {
		return nil, err
	}
	if !open {
		log.Printf("[workflow][run:%s] channel %s outbound window closed for entry=%s — message withheld",
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
// operator gets — it names the node, the run and the channel precisely, because
// "the workflow completed but the customer got nothing" is otherwise very hard
// to reconstruct.
func skipUnsupportedNode(ctx *workflow.NodeContext, nodeKind string) *workflow.NodeResult {
	log.Printf("[workflow][node:%s][run:%s] %s is not supported on channel %q — node skipped, run continues (entry=%s)",
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
