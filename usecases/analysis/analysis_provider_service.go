package analysis_usecase

import (
	"vozko/domain/analysis"
	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type analysisProviderService struct {
	repo analysis.Repository
}

func NewAnalysisProviderService(repo analysis.Repository) conversation.AnalysisProvider {
	return &analysisProviderService{repo: repo}
}

func (s *analysisProviderService) GetBatchLatestAnalysis(entryIDs []string, entryType string) (map[string]*analysis.Analysis, error) {
	return s.repo.FindLatestByEntries(entryIDs, shared.EntryType(entryType))
}
