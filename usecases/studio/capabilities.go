package studio_usecase

import (
	"context"

	"vozko/domain/studio"
)

type CapabilityService struct {
	reports studio.CapabilityRepository
}

func NewCapabilityService(reports studio.CapabilityRepository) (*CapabilityService, error) {
	if reports == nil {
		return nil, ErrMissingDependency
	}
	return &CapabilityService{reports: reports}, nil
}

func (s *CapabilityService) Report(ctx context.Context, r studio.CapabilityReport) error {
	report, err := studio.NewCapabilityReport(r)
	if err != nil {
		return err
	}
	return s.reports.Upsert(ctx, report)
}
