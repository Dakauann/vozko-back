package conversation

import "context"

// OperatorSendInput is one message a human is sending on a conversation.
//
// Exactly one shape is expressed per call: a plain text message, a text message
// carrying media, or an interactive button prompt. Buttons being a pointer is
// what keeps those three in one struct without a discriminator field that could
// disagree with the payload.
type OperatorSendInput struct {
	EntryID   string
	EntryType string

	// WorkspaceID is a hint used only for the post-send side effects; the
	// authoritative value is resolved from the entry.
	WorkspaceID string

	// SenderUserID is the operator the message is attributed to. On a scheduled
	// send this is whoever composed it, not whoever happens to be online when it
	// fires.
	SenderUserID string

	Text             string
	MediaID          string
	MediaType        string
	ReplyToMessageID string

	// Signed prefixes the text with the sender's name. Applied at send time
	// rather than at compose time so a deferred message carries the operator's
	// current display name.
	Signed bool

	// Buttons turns this into an interactive prompt. Nil for a normal send.
	Buttons *SendButtonInput
}

// OperatorSendUseCase delivers a message a human authored.
//
// It owns the whole of what "an operator sent this" means: resolving the
// sender, applying their signature in the form the channel renders, routing to
// the right send shape, and running every post-send side effect the
// conversation is owed.
//
// It is a use case rather than a method on the WebSocket hub because there is
// more than one way for an operator's message to reach the customer — the live
// composer sends now, the scheduled dispatcher sends later — and the second one
// must not re-derive the first one's rules. While this logic lived in the hub's
// frame handler, the signature format, the media routing and the five post-send
// effects were all reachable from exactly one transport.
type OperatorSendUseCase interface {
	Execute(ctx context.Context, in OperatorSendInput) (*Message, error)
}

// FinalizeOperatorSendInput describes one human reply that has just been
// delivered.
type FinalizeOperatorSendInput struct {
	EntryID   string
	EntryType string

	// WorkspaceID is a hint. Empty means "resolve it".
	WorkspaceID string

	ActorUserID string

	Message *Message
}

// OperatorSendFinalizer applies every side effect of a delivered human reply.
//
// A delivered operator message is never just a row: it moves the conversation
// status, writes a `replied` event on the activity timeline, ends any AI
// attendance session (a human just talked over the agent), and makes sure the
// entry carries its initial stage.
//
// Broadcasting is deliberately NOT among them. Telling connected clients is the
// transport's job, and the transport already does it — folding it in here would
// have made this port depend on the WebSocket hub that calls it.
//
// Implementations must be safe to call twice for the same message: the
// scheduled dispatcher's crash recovery can re-enter, and none of these effects
// is worth losing a delivered message over.
type OperatorSendFinalizer interface {
	FinalizeOperatorSend(ctx context.Context, in FinalizeOperatorSendInput) error
}
