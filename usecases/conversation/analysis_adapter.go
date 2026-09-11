package conversation_usecase

import (
	"context"
	"log"
	"strings"
	"time"

	ca "vozko/domain/comment_analysis"
	"vozko/domain/conversation"
	"vozko/domain/shared"
)

// The conversation side of the analysis engine: the ONE file that knows both
// conversation storage and the engine's ports.
//
// Inbound, Enqueue is what a channel calls when a conversation has gone quiet
// and is worth classifying. Outbound, it is the engine's ConversationAdapter:
// it renders the transcript back at classification time (the engine stores only
// an excerpt) and supplies the campaign's objective, which the rubric leans on
// because every criterion is written relative to "the conversation's
// objective".
//
// Nothing here is channel-specific. The transcript comes from
// conversation_messages, which every channel writes to, and the per-channel
// facts come from the AnalysisSubject resolvers that already exist. That is why
// this one adapter serves WhatsApp, Instagram, Telegram, unofficial WhatsApp
// and voice, instead of five.

const (
	// transcriptMessageLimit is how much of a conversation reaches the model.
	//
	// The same 100 the previous engine used, kept deliberately: it is a real
	// cost ceiling on a conversation that has run for months, and changing it
	// here would silently change what every analysis is based on. The engine's
	// token budget then decides how many such transcripts share one call.
	transcriptMessageLimit = 100

	// transcriptRuneLimit bounds one rendered transcript regardless of message
	// count, since a hundred long messages is still a very large prompt. The
	// budget planner sizes batches from the text it is given, so an unbounded
	// transcript would not overflow a call, it would simply push every batch
	// down to one item and multiply the number of calls.
	transcriptRuneLimit = 24_000
)

// AnalysisAdapter bridges conversations and the analysis engine.
type AnalysisAdapter struct {
	ingestor    ca.Ingestor
	messageRepo conversation.MessageRepository
	// resolvers load the per-channel facts. Same registry shape the debounce
	// job uses, so a channel is wired once and both see it.
	resolvers map[shared.EntryType]AnalysisSubjectResolver
	// objectives name what a container is FOR. Optional: without one the prompt
	// says the objective is unavailable and tells the model to be conservative
	// about judging progress, which is honest rather than silently inventing a
	// goal the conversation is then scored against.
	objectives map[shared.EntryType]ContainerObjectiveResolver
}

// ContainerObjectiveResolver names a container: the campaign objective a set of
// conversations is trying to reach.
type ContainerObjectiveResolver func(ctx context.Context, containerID string) (string, error)

func NewAnalysisAdapter(ingestor ca.Ingestor, messageRepo conversation.MessageRepository) *AnalysisAdapter {
	return &AnalysisAdapter{
		ingestor:    ingestor,
		messageRepo: messageRepo,
		resolvers:   map[shared.EntryType]AnalysisSubjectResolver{},
		objectives:  map[shared.EntryType]ContainerObjectiveResolver{},
	}
}

// RegisterResolver wires one channel.
func (a *AnalysisAdapter) RegisterResolver(entryType shared.EntryType, resolver AnalysisSubjectResolver) {
	if a == nil || resolver == nil {
		return
	}
	a.resolvers[entryType] = resolver
}

// ---- inbound ----

// Enqueue queues one conversation for analysis.
//
// Returns nil when the conversation should not be analysed (deleted, or its
// container has analysis switched off). That is a normal outcome and not an
// error, the same contract the resolvers themselves have.
func (a *AnalysisAdapter) Enqueue(ctx context.Context, entryID string, entryType shared.EntryType) error {
	if a == nil || a.ingestor == nil {
		return nil
	}
	subject, err := a.subject(ctx, entryID, entryType)
	if err != nil || subject == nil {
		return err
	}
	if !subject.EnableAnalysis {
		return nil
	}

	transcript, _, lastAt := a.render(entryID, entryType, subject.ContactLabel)
	if strings.TrimSpace(transcript) == "" {
		// Nothing said yet. Queuing it would spend a model call on an empty
		// conversation and store an analysis of nothing.
		return nil
	}

	return a.ingestor.Enqueue(ctx, ca.IngestInput{
		WorkspaceID: subject.WorkspaceID,
		Container: ca.ContainerRef{
			Kind:   ca.SubjectKindConversation,
			Source: ca.SourceOf(entryType),
			// The WORKSPACE stands in for the account here, and that is a
			// deliberate product decision rather than a missing field.
			//
			// Settings are keyed on (source, account). A comment's settings
			// belong to the Instagram account whose posts are being commented
			// on. A conversation has no equivalent: its configuration lives on
			// the campaign, which is the CONTAINER, and the container override
			// already carries per-campaign settings. So the account level here
			// is "this workspace on this channel", which is the level an
			// operator actually thinks at when switching conversation analysis
			// on for WhatsApp.
			AccountID:   subject.WorkspaceID,
			ContainerID: subject.ContainerID,
		},
		SourceCommentID:  entryID,
		AuthorExternalID: subject.ContactLabel,
		AuthorHandle:     subject.ContactLabel,
		Text:             transcript,
		CommentedAt:      lastAt,
	})
}

// ---- outbound: the engine's ConversationAdapter ----

// ReadTranscripts renders each conversation. Ids that no longer resolve are
// absent from the map, and the engine skips those rows rather than failing
// them, exactly as it does for a deleted comment.
func (a *AnalysisAdapter) ReadTranscripts(ctx context.Context, ref ca.ContainerRef, entryIDs []string) (map[string]ca.Transcript, error) {
	entryType := ref.Source.EntryType()
	out := make(map[string]ca.Transcript, len(entryIDs))

	for _, entryID := range entryIDs {
		subject, err := a.subject(ctx, entryID, entryType)
		if err != nil {
			// One unreadable conversation must not fail the batch its peers are
			// in. It is left out, so the engine skips it and the others run.
			log.Printf("[analysis-adapter] %s entry %s unreadable: %v", entryType, entryID, err)
			continue
		}
		label := ""
		if subject != nil {
			label = subject.ContactLabel
		}
		text, count, lastAt := a.render(entryID, entryType, label)
		if strings.TrimSpace(text) == "" {
			continue
		}
		out[entryID] = ca.Transcript{Text: text, MessageCount: count, LastMessageAt: lastAt}
	}
	return out, nil
}

// ReadContainerContext describes what the conversations are FOR. The container
// name is the campaign's, and it reaches the prompt as the objective.
func (a *AnalysisAdapter) ReadContainerContext(ctx context.Context, ref ca.ContainerRef) (ca.ContainerContext, error) {
	resolve, ok := a.objectives[ref.Source.EntryType()]
	if !ok || resolve == nil {
		return ca.ContainerContext{}, nil
	}
	objective, err := resolve(ctx, ref.ContainerID)
	if err != nil {
		// The objective helps; its absence is not a reason to stop analysing.
		return ca.ContainerContext{}, nil
	}
	return ca.ContainerContext{Caption: strings.TrimSpace(objective)}, nil
}

// RegisterObjective wires one channel's container naming.
func (a *AnalysisAdapter) RegisterObjective(entryType shared.EntryType, resolve ContainerObjectiveResolver) {
	if a == nil || resolve == nil {
		return
	}
	a.objectives[entryType] = resolve
}

// ---- helpers ----

func (a *AnalysisAdapter) subject(ctx context.Context, entryID string, entryType shared.EntryType) (*AnalysisSubject, error) {
	resolver, ok := a.resolvers[entryType]
	if !ok || resolver == nil {
		return nil, nil
	}
	return resolver(ctx, entryID)
}

// render builds the transcript the model reads, reusing the same renderer the
// prompt builder has always used so the wording of a turn is defined once.
func (a *AnalysisAdapter) render(entryID string, entryType shared.EntryType, contactLabel string) (string, int, time.Time) {
	if a.messageRepo == nil {
		return "", 0, time.Time{}
	}
	history, err := a.messageRepo.ListByEntry(entryID, entryType)
	if err != nil || len(history) == 0 {
		return "", 0, time.Time{}
	}

	// Total BEFORE the window, so the stored count says how long the
	// conversation actually is rather than how much of it we read.
	total := len(history)
	if len(history) > transcriptMessageLimit {
		history = history[len(history)-transcriptMessageLimit:]
	}

	var lastAt time.Time
	if last := history[len(history)-1]; last != nil {
		lastAt = last.CreatedAt
	}

	text, _ := ca.TruncateRunes(BuildTranscript(history, contactLabel), transcriptRuneLimit)
	return text, total, lastAt
}
