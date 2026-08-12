package conversation

import (
	"fmt"
	"strings"

	"vozko/domain/shared"
)

// SignOutbound prefixes an operator's message with their name.
//
// WhatsApp renders *bold* markup; Instagram DMs do not, so the same asterisks
// would be shown literally to the customer. The format is therefore chosen per
// channel rather than hardcoded.
//
// It lives in the domain because more than one send surface produces an
// operator message: the WebSocket composer sends now, the scheduled dispatcher
// sends later, and both must put byte-identical text on the wire. When this was
// a private helper on the WebSocket hub, the second surface's only options were
// to copy it or to silently drop the signature.
//
// An empty username yields the text unchanged rather than an empty prefix: the
// hub resolves the user with resolve-and-continue, so a lookup failure must cost
// the signature, not corrupt the message.
func SignOutbound(entryType shared.EntryType, username, text string) string {
	username = strings.TrimSpace(username)
	if username == "" {
		return text
	}
	if entryType == shared.EntryTypeInstagram {
		return fmt.Sprintf("%s:\n%s", username, text)
	}
	return fmt.Sprintf("*%s*:\n%s", username, text)
}
