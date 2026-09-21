package tools_usecase

import "strings"

var legacyToolNames = map[string]string{
	"send_whatsapp_media":           ToolNameSendMedia,
	LegacyToolNameSendWhatsappImage: ToolNameSendMedia,
	"send_whatsapp_button_message":  ToolNameSendOptions,
}

func CanonicalToolName(name string) string {
	key := strings.ToLower(strings.TrimSpace(name))
	if current, ok := legacyToolNames[key]; ok {
		return current
	}
	return key
}
