package tools_usecase

import "strings"

// Tool names are persisted: an agent's InternalTools bindings store the name the
// operator selected, and workflow configs reference them too. Renaming a tool
// therefore breaks every saved binding, the resolver looks the name up in its
// definition index, misses, and `continue`s. The tool simply vanishes from the
// agent with no error anywhere.
//
// This maps retired names onto current ones so a rename is a rename, not a
// silent unbinding. Entries are permanent: a workspace that has not touched its
// agent since the rename still holds the old name.
var legacyToolNames = map[string]string{
	// Renamed when the tool stopped being WhatsApp-only. The old name is also
	// what the MODEL saw, and a tool called "send_whatsapp_media" offered inside
	// a Telegram conversation reads as inapplicable, the rename is a behaviour
	// fix, not just tidying.
	"send_whatsapp_media":          ToolNameSendMedia,
	LegacyToolNameSendWhatsappImage: ToolNameSendMedia,
	"send_whatsapp_button_message": ToolNameSendOptions,
}

// CanonicalToolName resolves a possibly-retired tool name to its current one.
func CanonicalToolName(name string) string {
	key := strings.ToLower(strings.TrimSpace(name))
	if current, ok := legacyToolNames[key]; ok {
		return current
	}
	return key
}
