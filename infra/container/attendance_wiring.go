package container

import (
	"context"
	"strings"
	"time"

	domainCache "vozko/domain/cache"
	"vozko/domain/conversation"
	wsc "vozko/domain/workspace_config"
	redisCache "vozko/infra/cache"
	attendance_usecase "vozko/usecases/attendance"
	conversation_usecase "vozko/usecases/conversation"
)

type outcomeCaptureReader struct {
	configs wsc.Repository
}

func (r outcomeCaptureReader) OutcomeCaptureFor(ctx context.Context, workspaceID string) (*conversation.OutcomeCapture, error) {
	if r.configs == nil || strings.TrimSpace(workspaceID) == "" {
		return nil, nil
	}
	config, err := r.configs.GetByWorkspaceID(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	if config == nil {
		return nil, nil
	}
	return config.OutcomeCapture, nil
}

var _ conversation_usecase.OutcomeCaptureReader = outcomeCaptureReader{}

type entryDepartmentLookup interface {
	GetEntryDepartmentID(entryID, entryType string) (string, error)
}

type entryDepartmentResolver struct {
	lookup entryDepartmentLookup
}

func (r entryDepartmentResolver) DepartmentIDForEntry(ctx context.Context, entryID, entryType string) (string, error) {
	if r.lookup == nil {
		return "", nil
	}
	return r.lookup.GetEntryDepartmentID(entryID, entryType)
}

var _ conversation_usecase.EntryDepartmentResolver = entryDepartmentResolver{}

func (c *Container) attendanceScheduleResolver() *attendance_usecase.ScheduleResolver {
	return attendance_usecase.NewScheduleResolver(
		c.repositories.workspaceConfig,
		c.repositories.workspaceDepartment,
	)
}

const (
	analyticsGateCapacity       = 3
	analyticsGateMaxWait        = 20 * time.Second
	analyticsComputeTimeout     = 30 * time.Second
	attendanceSectionTTL        = 60 * time.Second
	attendanceSectionVersionKey = "attendance:overview"
)

func (c *Container) sharedAnalyticsGate() domainCache.Gate {
	if c.analyticsGate == nil {
		c.analyticsGate = redisCache.NewSemaphoreGate(analyticsGateCapacity, analyticsGateMaxWait)
	}
	return c.analyticsGate
}

func (c *Container) analyticsMemo() domainCache.Memo {
	return redisCache.NewSharedStateMemo(c.redisProvider.SharedState(), analyticsComputeTimeout)
}

func (c *Container) attendanceSectionVersions() domainCache.Versions {
	return redisCache.NewSharedStateVersions(c.redisProvider.SharedState(), attendanceSectionVersionKey)
}

func (c *Container) attendanceSectionCaching() attendance_usecase.SectionCaching {
	return attendance_usecase.SectionCaching{
		Memo:     c.analyticsMemo(),
		Gate:     c.sharedAnalyticsGate(),
		Versions: c.attendanceSectionVersions(),
		TTL:      attendanceSectionTTL,
	}
}

func (c *Container) attendanceTargetsService() *attendance_usecase.TargetsService {
	service := attendance_usecase.NewTargetsService(
		c.repositories.attendanceTarget,
		c.attendanceScheduleResolver(),
	)
	service.SetVersions(c.attendanceSectionVersions())
	return service
}

func (c *Container) wireOutcomeCapture() {
	service := c.services.conversationStatusService
	if service == nil {
		return
	}
	service.SetOutcomeCaptureReader(outcomeCaptureReader{configs: c.repositories.workspaceConfig})
	if lookup, ok := c.services.campaignWorkspaceResolver.(entryDepartmentLookup); ok {
		service.SetEntryDepartmentResolver(entryDepartmentResolver{lookup: lookup})
	}
}
