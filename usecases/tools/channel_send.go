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
	window, err := adapter.WindowState(ctx, ec)
	if err != nil {
		return err
	}
	if !window.Open {
		return fmt.Errorf("a janela de resposta do canal %s está fechada; o contato precisa escrever novamente", ec.EntryType)
	}
	return nil
}

func toolRefusal(reason string) tools.ExecutionResult {
	return tools.ExecutionResult{Result: reason, IsError: true, ContextUpdateText: reason}
}

func adapterMediaKind(item *media.Media) string {
	switch mediaKindFor(item.Type) {
	case kindImage:
		return "image"
	case kindVideo:
		return "video"
	case kindAudio:
		return "audio"
	default:
		return "document"
	}
}
