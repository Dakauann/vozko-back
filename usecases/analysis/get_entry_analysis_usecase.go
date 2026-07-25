package analysis_usecase

import (
	"vozko/domain/analysis"
	"vozko/domain/shared"
)

type getEntryAnalysisUseCase struct {
	repo analysis.Repository
}

func NewGetEntryAnalysisUseCase(repo analysis.Repository) analysis.GetEntryAnalysisUseCase {
	return &getEntryAnalysisUseCase{repo: repo}
}

func (uc *getEntryAnalysisUseCase) Execute(entryID string, entryType shared.EntryType) (*analysis.Analysis, error) {
	return uc.repo.FindLatestByEntry(entryID, entryType)
}
