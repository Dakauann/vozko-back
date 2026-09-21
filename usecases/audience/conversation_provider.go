package audience_usecase

import (
	"context"

	ca "vozko/domain/audience"
	"vozko/domain/conversation"
	"vozko/domain/shared"
)

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
