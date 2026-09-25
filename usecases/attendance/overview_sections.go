package attendance_usecase

import (
	"context"
	"time"

	"vozko/domain/attendance"
	"vozko/domain/cache"
)

const sectionCachePrefix = "attendance:overview"

type SectionCaching struct {
	Memo     cache.Memo
	Gate     cache.Gate
	Versions cache.Versions
	TTL      time.Duration
}

func (uc *getOverviewUseCase) SetCaching(caching SectionCaching) {
	if uc == nil {
		return
	}
	uc.memo = caching.Memo
	uc.gate = caching.Gate
	uc.versions = caching.Versions
	uc.ttl = caching.TTL
}

func (uc *getOverviewUseCase) Summary(
	ctx context.Context,
	workspaceID string,
	filter attendance.OverviewFilter,
) (*attendance.SummarySection, error) {
	return rememberSection(ctx, uc, workspaceID, attendance.SectionSummary, filter,
		func(ctx context.Context) (*attendance.SummarySection, error) {
			var out *attendance.SummarySection
			err := cache.Gated(ctx, uc.gate, func(ctx context.Context) error {
				var err error
				out, err = uc.computeSummary(ctx, workspaceID, filter)
				return err
			})
			return out, err
		})
}

func (uc *getOverviewUseCase) Trend(
	ctx context.Context,
	workspaceID string,
	filter attendance.OverviewFilter,
) (*attendance.TrendSection, error) {
	return rememberSection(ctx, uc, workspaceID, attendance.SectionTrend, filter,
		func(ctx context.Context) (*attendance.TrendSection, error) {
			summary, err := uc.Summary(ctx, workspaceID, filter)
			if err != nil {
				return nil, err
			}
			out := &attendance.TrendSection{}
			err = cache.Gated(ctx, uc.gate, func(ctx context.Context) error {
				window, err := uc.loadWindow(ctx, workspaceID, filter)
				if err != nil {
					return err
				}
				out.Trend, err = uc.buildTrend(ctx, workspaceID, filter, window, summary)
				return err
			})
			return out, err
		})
}

func (uc *getOverviewUseCase) Team(
	ctx context.Context,
	workspaceID string,
	filter attendance.OverviewFilter,
) (*attendance.TeamSection, error) {
	return rememberSection(ctx, uc, workspaceID, attendance.SectionTeam, filter,
		func(ctx context.Context) (*attendance.TeamSection, error) {
			summary, err := uc.Summary(ctx, workspaceID, filter)
			if err != nil {
				return nil, err
			}
			var out *attendance.TeamSection
			err = cache.Gated(ctx, uc.gate, func(ctx context.Context) error {
				window, err := uc.loadWindow(ctx, workspaceID, filter)
				if err != nil {
					return err
				}
				if out, err = uc.repo.ReadTeam(ctx, workspaceID, filter); err != nil {
					return err
				}
				out.TeamRanking = uc.buildTeamRanking(workspaceID, filter, window, summary, out.ByMember)
				return nil
			})
			return out, err
		})
}

func (uc *getOverviewUseCase) Stages(
	ctx context.Context,
	workspaceID string,
	filter attendance.OverviewFilter,
) (*attendance.StagesSection, error) {
	return rememberSection(ctx, uc, workspaceID, attendance.SectionStages, filter,
		func(ctx context.Context) (*attendance.StagesSection, error) {
			out := &attendance.StagesSection{}
			err := cache.Gated(ctx, uc.gate, func(ctx context.Context) error {
				var err error
				out.Stages, err = uc.repo.ReadStages(ctx, workspaceID, filter)
				return err
			})
			return out, err
		})
}

func (uc *getOverviewUseCase) Backlog(
	ctx context.Context,
	workspaceID string,
	filter attendance.OverviewFilter,
) (*attendance.BacklogSection, error) {
	return rememberSection(ctx, uc, workspaceID, attendance.SectionBacklog, filter,
		func(ctx context.Context) (*attendance.BacklogSection, error) {
			out := &attendance.BacklogSection{}
			err := cache.Gated(ctx, uc.gate, func(ctx context.Context) error {
				var err error
				out.BacklogXray, err = uc.repo.ReadBacklog(ctx, workspaceID, filter, uc.clock())
				return err
			})
			return out, err
		})
}

func (uc *getOverviewUseCase) Rework(
	ctx context.Context,
	workspaceID string,
	filter attendance.OverviewFilter,
) (*attendance.ReworkSection, error) {
	return rememberSection(ctx, uc, workspaceID, attendance.SectionRework, filter,
		func(ctx context.Context) (*attendance.ReworkSection, error) {
			out := &attendance.ReworkSection{}
			err := cache.Gated(ctx, uc.gate, func(ctx context.Context) error {
				var err error
				out.Rework, err = uc.repo.ReadRework(ctx, workspaceID, filter)
				return err
			})
			return out, err
		})
}

func rememberSection[T any](
	ctx context.Context,
	uc *getOverviewUseCase,
	workspaceID string,
	section attendance.Section,
	filter attendance.OverviewFilter,
	compute func(context.Context) (T, error),
) (T, error) {
	if uc.memo == nil || uc.versions == nil {
		return compute(ctx)
	}
	version, err := uc.versions.Version(workspaceID)
	if err != nil {
		return compute(ctx)
	}
	key := sectionCachePrefix + ":" + workspaceID + ":" + version + ":" + filter.Fingerprint(section)
	return cache.Remember(ctx, uc.memo, key, uc.ttl, compute)
}

func (uc *getOverviewUseCase) Live(
	_ context.Context,
	workspaceID string,
	filter attendance.OverviewFilter,
) (*attendance.LiveSection, error) {
	out := uc.liveSection(workspaceID, filter)
	return &out, nil
}
