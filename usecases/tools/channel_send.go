package tools_usecase

import (
	"context"
	"fmt"
	"log"
	"path"
	"strings"

	"vozko/domain/conversation"
	"vozko/domain/media"
	"vozko/domain/shared"
	"vozko/domain/tools"
)

// The messaging tools address WhatsApp by phone number. Every other channel
// addresses a conversation, so these resolve the recipient from the seeds the
// agent turn already stamps (__entry_id / __entry_type) and send through that
// channel's adapter.
//
// WhatsApp deliberately keeps its own path: it resolves a business phone,
// normalises images, falls back to a link when an upload fails, and checks the
// lead window, none of which generalises.

// resolveToolAdapter returns the channel adapter for the conversation this tool
// call belongs to.
//
// Reports false for WhatsApp (which has its own path), for a conversation with
// no seeds, and when no adapter is registered, in each case the caller falls
// back to the WhatsApp path and fails honestly there rather than here.
func resolveToolAdapter(
	ctx context.Context,
	registry conversation.AdapterRegistry,
	config map[string]interface{},
) (conversation.ChannelAdapter, *conversation.EntryContext, bool) {
	if registry == nil {
		return nil, nil, false
	}
	entryID, _ := config["__entry_id"].(string)
	entryTypeStr, _ := config["__entry_type"].(string)
	entryID, entryTypeStr = strings.TrimSpace(entryID), strings.TrimSpace(entryTypeStr)
	if entryID == "" || entryTypeStr == "" {
		return nil, nil, false
	}

	entryType := shared.EntryType(entryTypeStr)
	if entryType == shared.EntryTypeWhatsApp {
		return nil, nil, false
	}

	adapter, err := registry.For(entryType)
	if err != nil || adapter == nil {
		return nil, nil, false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ec, err := adapter.ResolveEntry(ctx, entryID)
	if err != nil || ec == nil {
		log.Printf("[tools] could not resolve %s entry %s: %v", entryType, entryID, err)
		return nil, nil, false
	}
	return adapter, ec, true
}

// sendMediaViaAdapter delivers a stored media item on any adapter-backed
// channel.
func sendMediaViaAdapter(
	ctx context.Context,
	adapter conversation.ChannelAdapter,
	ec *conversation.EntryContext,
	item *media.Media,
	caption string,
) (tools.ExecutionResult, error) {
	if err := assertWindowOpen(ctx, adapter, ec); err != nil {
		return toolRefusal(err.Error()), nil
	}

	outcome, err := adapter.SendMedia(ctx, ec, conversation.SendMediaRequest{
		Kind:     adapterMediaKind(item),
		URL:      item.URL,
		FileName: path.Base(item.URL),
		Caption:  caption,
	})
	if err != nil {
		log.Printf("[tools] media send failed on %s: %v", ec.EntryType, err)
		return tools.ExecutionResult{}, err
	}

	providerID := ""
	if outcome != nil {
		providerID = outcome.ProviderMessageID
	}
	log.Printf("[tools] media sent on %s entry=%s id=%s", ec.EntryType, ec.EntryID, providerID)
	return tools.ExecutionResult{Result: map[string]interface{}{
		"success":    true,
		"message_id": providerID,
		"channel":    string(ec.EntryType),
	}}, nil
}

// sendOptionsViaAdapter delivers a single-choice prompt on channels that can
// present one. A channel without the capability says so plainly: sending the
// question without its options would leave the contact nothing to tap.
func sendOptionsViaAdapter(
	ctx context.Context,
	adapter conversation.ChannelAdapter,
	ec *conversation.EntryContext,
	req conversation.SendInteractiveRequest,
) (tools.ExecutionResult, error) {
	interactive, ok := adapter.(conversation.InteractiveAdapter)
	if !ok {
		return toolRefusal(fmt.Sprintf(
			"O canal %s não suporta botões. Responda em texto listando as opções.", ec.EntryType)), nil
	}
	if err := assertWindowOpen(ctx, adapter, ec); err != nil {
		return toolRefusal(err.Error()), nil
	}

	outcome, err := interactive.SendInteractive(ctx, ec, req)
	if err != nil {
		log.Printf("[tools] options send failed on %s: %v", ec.EntryType, err)
		return tools.ExecutionResult{}, err
	}

	providerID := ""
	if outcome != nil {
		providerID = outcome.ProviderMessageID
	}
	log.Printf("[tools] options sent on %s entry=%s options=%d id=%s",
		ec.EntryType, ec.EntryID, len(req.Options), providerID)
	return tools.ExecutionResult{Result: map[string]interface{}{
		"success":    true,
		"message_id": providerID,
		"channel":    string(ec.EntryType),
	}}, nil
}

func assertWindowOpen(ctx context.Context, adapter conversation.ChannelAdapter, ec *conversation.EntryContext) error {
	open, _, err := adapter.WindowState(ctx, ec)
	if err != nil {
		return err
	}
	if !open {
		return fmt.Errorf("a janela de resposta do canal %s está fechada; o contato precisa escrever novamente", ec.EntryType)
	}
	return nil
}

// toolRefusal is a non-fault outcome the model can act on. Returned as a
// successful execution with IsError so the turn continues, the agent can
// explain in text instead of the run failing.
func toolRefusal(reason string) tools.ExecutionResult {
	return tools.ExecutionResult{Result: reason, IsError: true, ContextUpdateText: reason}
}

// adapterMediaKind maps our stored media type onto the adapters' vocabulary.
func adapterMediaKind(item *media.Media) string {
	switch mediaKindFor(item.Type) {
	case kindImage:
		return "image"
	case kindVideo:
		return "video"
	case kindAudio:
		return "audio"
	default:
		// Stickers have no cross-channel equivalent; a document is the honest
		// fallback and every adapter accepts one.
		return "document"
	}
}
