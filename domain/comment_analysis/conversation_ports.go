package comment_analysis

import (
	"context"
	"time"
)

// What the engine needs from a channel in order to analyse a conversation.
//
// This is a separate, narrower port rather than more methods on SourceAdapter.
// Two of that interface's four methods are backfill machinery for public
// comments (enumerate an account's posts, page a post's comments off the
// provider) and neither has a meaning for a conversation: there is no edge to
// page and no container to enumerate. Widening SourceAdapter would force every
// channel to stub two methods it can never implement, which is how an
// interface stops describing anything.

// Transcript is one conversation rendered for the model.
type Transcript struct {
	// Text is the rendered exchange, already bounded by the adapter. The engine
	// does not know how a channel spells a turn.
	Text string
	// MessageCount is what the transcript covers, which is not necessarily the
	// conversation's whole history: a long conversation is rendered from its
	// most recent messages, and the count records what was actually read.
	MessageCount int
	// LastMessageAt buckets the row in the daily rollups, the way a comment's
	// posted-at timestamp does.
	LastMessageAt time.Time
}

// ConversationAdapter reads conversations for analysis.
type ConversationAdapter interface {
	// ReadTranscripts returns the transcript of each conversation by entry id.
	// Ids that no longer resolve are simply absent, exactly as a deleted
	// comment is absent from ReadTexts, and the engine skips those rows rather
	// than failing them.
	ReadTranscripts(ctx context.Context, ref ContainerRef, entryIDs []string) (map[string]Transcript, error)

	// ReadContainerContext describes what the conversations are FOR: the
	// campaign's name and objective. It is the conversation's equivalent of a
	// post's caption, and the rubric leans on it, since every criterion is
	// written relative to "the conversation's objective".
	ReadContainerContext(ctx context.Context, ref ContainerRef) (ContainerContext, error)
}
