package analytics_usecase

import analytics_domain "vozko/domain/analytics"

type getProfitReportUseCase struct {
	repo analytics_domain.Repository
}

func NewGetProfitReportUseCase(repo analytics_domain.Repository) analytics_domain.GetProfitReportUseCase {
	return &getProfitReportUseCase{repo: repo}
}

func (uc *getProfitReportUseCase) Execute(input analytics_domain.ProfitReportInput) (*analytics_domain.ProfitReport, error) {
	return uc.repo.GetProfitReport(input)
}

type getCallAnalyticsUseCase struct {
	repo analytics_domain.Repository
}

func NewGetCallAnalyticsUseCase(repo analytics_domain.Repository) analytics_domain.GetCallAnalyticsUseCase {
	return &getCallAnalyticsUseCase{repo: repo}
}

func (uc *getCallAnalyticsUseCase) Execute(input analytics_domain.CallAnalyticsInput) (*analytics_domain.CallAnalyticsReport, error) {
	return uc.repo.GetCallAnalytics(input)
}
