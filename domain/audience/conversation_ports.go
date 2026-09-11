package audience

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

// ---- Reading conversation analyses from outside the engine ----

// ConversationReader is what the rest of the system asks about conversations:
// the inbox showing a verdict beside a thread, the export writing it into a
// column, the lead screen listing a campaign's conversations.
//
// It is a separate interface from Repository rather than three more methods on
// it. Repository is the ENGINE's port, implemented by the engine's fakes; these
// callers need two reads and have no business holding the queue, the claim or
// the purge.
//
// "Latest" is load-bearing. A conversation has a TIMELINE of analyses, one per
// revision of its transcript, so these reads answer with the most recent
// COMPLETED one: a screen here shows a verdict, and a revision still in the
// queue must never blank out the answer already on it.
type ConversationReader interface {
	// LatestByEntries answers for many conversations at once. Ids with no
	// analysis are absent rather than present-and-empty, so a caller can tell
	// "not analysed yet" from "analysed and found nothing".
	LatestByEntries(ctx context.Context, workspaceID string, source Source, entryIDs []string) (map[string]*Analysis, error)

	// LatestByEntry answers for one.
	LatestByEntry(ctx context.Context, workspaceID string, source Source, entryID string) (*Analysis, error)

	// PendingByEntries reports which of these conversations have analysis
	// WAITING: queued or in flight, nothing finished for that revision yet.
	//
	// It is a separate read from LatestByEntries because it answers a different
	// question, and folding them together would mean either showing an
	// unfinished row as a verdict or losing the last good one while the next is
	// computed. Ids with nothing pending are absent rather than false, so the
	// map is the size of the answer and not of the page.
	PendingByEntries(ctx context.Context, workspaceID string, source Source, entryIDs []string) (map[string]bool, error)
}
