package audience_usecase

import (
	"context"

	ca "vozko/domain/audience"
	"vozko/domain/conversation"
	"vozko/domain/shared"
)

// The inbox's view of a conversation's analysis.
//
// This replaces a service that looped a query per entry from the websocket
// path; LatestByEntries answers the whole page in one. The workspace is not
// available at this seam, so the scope is the entry ids themselves, which the
// caller has already resolved inside the session's workspace.

type conversationAnalysisProvider struct {
	reader ca.ConversationReader
}

func NewConversationAnalysisProvider(reader ca.ConversationReader) conversation.AnalysisProvider {
	return &conversationAnalysisProvider{reader: reader}
}

func (p *conversationAnalysisProvider) GetBatchLatestAnalysis(entryIDs []string, entryType string) (map[string]*ca.Analysis, error) {
	if p == nil || p.reader == nil || len(entryIDs) == 0 {
		return map[string]*ca.Analysis{}, nil
	}
	return p.reader.LatestByEntries(
		context.Background(), "", ca.SourceOf(shared.EntryType(entryType)), entryIDs)
}

func (p *conversationAnalysisProvider) GetBatchAnalysisPending(entryIDs []string, entryType string) (map[string]bool, error) {
	if p == nil || p.reader == nil || len(entryIDs) == 0 {
		return map[string]bool{}, nil
	}
	return p.reader.PendingByEntries(
		context.Background(), "", ca.SourceOf(shared.EntryType(entryType)), entryIDs)
}
