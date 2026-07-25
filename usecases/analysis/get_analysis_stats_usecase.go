package analysis_usecase

import (
	"vozko/domain/analysis"
)

type getAnalysisStatsUseCase struct {
	repo analysis.Repository
}

func NewGetAnalysisStatsUseCase(repo analysis.Repository) analysis.GetAnalysisStatsUseCase {
	return &getAnalysisStatsUseCase{repo: repo}
}

func (uc *getAnalysisStatsUseCase) Execute(input analysis.ListAnalysisInput) (*analysis.AnalysisStats, error) {
	return uc.repo.GetStats(input)
}
