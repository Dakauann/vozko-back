package analysis_usecase

import (
	"vozko/domain/analysis"
	"vozko/domain/shared"
)

type listAnalysisUseCase struct {
	repo analysis.Repository
}

func NewListAnalysisUseCase(repo analysis.Repository) analysis.ListAnalysisUseCase {
	return &listAnalysisUseCase{repo: repo}
}

func (uc *listAnalysisUseCase) Execute(input analysis.ListAnalysisInput) (*shared.PaginatedResult[*analysis.Analysis], error) {
	return uc.repo.List(input)
}
