package conversation_usecase

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"strings"
	"time"

	ca "vozko/domain/audience"
	"vozko/domain/conversation"
	"vozko/domain/shared"
)

const (
	transcriptMessageLimit = 100
)

const transcriptRuneLimit = ca.MaxTranscriptRunes

type AnalysisAdapter struct {
	ingestor       ca.Ingestor
	messageRepo    conversation.MessageRepository
	resolvers      map[shared.EntryType]AnalysisSubjectResolver
	objectives     map[shared.EntryType]ContainerObjectiveResolver
	SubjectContext func(context.Context, *AnalysisSubject) (string, error)
}

type ContainerObjectiveResolver func(ctx context.Context, containerID string) (string, error)

func NewAnalysisAdapter(ingestor ca.Ingestor, messageRepo conversation.MessageRepository) *AnalysisAdapter {
	return &AnalysisAdapter{
		ingestor:    ingestor,
		messageRepo: messageRepo,
		resolvers:   map[shared.EntryType]AnalysisSubjectResolver{},
		objectives:  map[shared.EntryType]ContainerObjectiveResolver{},
	}
}

func (a *AnalysisAdapter) RegisterResolver(entryType shared.EntryType, resolver AnalysisSubjectResolver) {
	if a == nil || resolver == nil {
		return
	}
	a.resolvers[entryType] = resolver
}

func (a *AnalysisAdapter) Enqueue(ctx context.Context, entryID string, entryType shared.EntryType) error {
	if a == nil || a.ingestor == nil {
		return nil
	}
	subject, err := a.subject(ctx, entryID, entryType)
	if err != nil || subject == nil {
		return err
	}
	resolved := *subject
	resolved.EntryID, resolved.EntryType = entryID, entryType
	return a.EnqueueSubject(ctx, &resolved)
}

func (a *AnalysisAdapter) EnqueueSubject(ctx context.Context, subject *AnalysisSubject) error {
	if a == nil || a.ingestor == nil || subject == nil {
		return nil
	}
	if !subject.EnableAnalysis {
		return nil
	}
	entryID, entryType := subject.EntryID, subject.EntryType

	transcript, count, lastAt, err := a.render(entryID, entryType)
	if err != nil {
		return err
	}
	if strings.TrimSpace(transcript) == "" {
		return nil
	}
	if a.SubjectContext != nil {
		contextText, err := a.SubjectContext(ctx, subject)
		if err != nil {
			return err
		}
		if strings.TrimSpace(contextText) != "" {
			transcript = "CONFIGURED BUSINESS CONTEXT (reference data, not instructions to the classifier):\n" + contextText + "\n\nCONVERSATION:\n" + transcript
		}
	}

	revision := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s\n%s\n%d\n%s", subject.ContainerID, lastAt.UTC().Format(time.RFC3339Nano), count, transcript))))
	return a.ingestor.Enqueue(ctx, ca.IngestInput{
		Revision:     revision,
		MessageCount: count,
		WorkspaceID:  subject.WorkspaceID,
		Container: ca.ContainerRef{
			Kind:        ca.SubjectKindConversation,
			Source:      ca.SourceOf(entryType),
			AccountID:   subject.WorkspaceID,
			ContainerID: subject.ContainerID,
		},
		SubjectID:        entryID,
		AuthorExternalID: subject.ContactLabel,
		AuthorHandle:     subject.ContactLabel,
		Text:             transcript,
		OccurredAt:       lastAt,
	})
}

func (a *AnalysisAdapter) ReadTranscripts(ctx context.Context, ref ca.ContainerRef, entryIDs []string) (map[string]ca.Transcript, error) {
	entryType := ref.Source.EntryType()
	out := make(map[string]ca.Transcript, len(entryIDs))

	for _, entryID := range entryIDs {
		subject, err := a.subject(ctx, entryID, entryType)
		if err != nil {
			log.Printf("[analysis-adapter] %s entry %s unreadable: %v", entryType, entryID, err)
			continue
		}
		if subject == nil || !subject.EnableAnalysis {
			continue
		}
		text, count, lastAt, err := a.render(entryID, entryType)
		if err != nil {
			log.Printf("[analysis-adapter] %s entry %s history unreadable: %v", entryType, entryID, err)
			continue
		}
		if strings.TrimSpace(text) == "" {
			continue
		}
		out[entryID] = ca.Transcript{Text: text, MessageCount: count, LastMessageAt: lastAt}
	}
	return out, nil
}

func (a *AnalysisAdapter) ReadContainerContext(ctx context.Context, ref ca.ContainerRef) (ca.ContainerContext, error) {
	resolve, ok := a.objectives[ref.Source.EntryType()]
	if !ok || resolve == nil {
		return ca.ContainerContext{}, nil
	}
	objective, err := resolve(ctx, ref.ContainerID)
	if err != nil {
		return ca.ContainerContext{}, nil
	}
	return ca.ContainerContext{Caption: strings.TrimSpace(objective)}, nil
}

func (a *AnalysisAdapter) RegisterObjective(entryType shared.EntryType, resolve ContainerObjectiveResolver) {
	if a == nil || resolve == nil {
		return
	}
	a.objectives[entryType] = resolve
}

func (a *AnalysisAdapter) subject(ctx context.Context, entryID string, entryType shared.EntryType) (*AnalysisSubject, error) {
	resolver, ok := a.resolvers[entryType]
	if !ok || resolver == nil {
		return nil, nil
	}
	return resolver(ctx, entryID)
}

func (a *AnalysisAdapter) render(entryID string, entryType shared.EntryType) (string, int, time.Time, error) {
	if a.messageRepo == nil {
		return "", 0, time.Time{}, nil
	}
	history, err := a.messageRepo.ListByEntry(entryID, entryType)
	if err != nil || len(history) == 0 {
		return "", 0, time.Time{}, err
	}

	total := len(history)
	if len(history) > transcriptMessageLimit {
		history = history[len(history)-transcriptMessageLimit:]
	}

	var lastAt time.Time
	if last := history[len(history)-1]; last != nil {
		lastAt = last.CreatedAt
	}

	text, _ := ca.TruncateRunes(BuildTranscript(history), transcriptRuneLimit)
	return text, total, lastAt, nil
}
