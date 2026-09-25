package attendance_usecase

import (
	"context"
	"math"
	"time"

	"vozko/domain/agent_presence"
	"vozko/domain/attendance"
	"vozko/domain/cache"
	"vozko/domain/callsession"
	"vozko/domain/queue_event"
)

type getOverviewUseCase struct {
	repo      attendance.Repository
	queue     queue_event.Repository
	presence  agent_presence.Repository
	live      callsession.CallSessionRegistry
	schedules *ScheduleResolver
	targets   *TargetsService
	memo      cache.Memo
	gate      cache.Gate
	versions  cache.Versions
	ttl       time.Duration
	now       func() time.Time
}

type OverviewService interface {
	attendance.GetOverviewUseCase
	attendance.OverviewSectionsUseCase
}

func NewGetOverviewUseCase(repo attendance.Repository) OverviewService {
	return &getOverviewUseCase{repo: repo, now: nowUTC}
}

func NewGetOverviewUseCaseWithDeps(
	repo attendance.Repository,
	queue queue_event.Repository,
	presence agent_presence.Repository,
	live callsession.CallSessionRegistry,
) OverviewService {
	return &getOverviewUseCase{
		repo:     repo,
		queue:    queue,
		presence: presence,
		live:     live,
		now:      nowUTC,
	}
}

func nowUTC() time.Time {
	return time.Now().UTC()
}

func (uc *getOverviewUseCase) SetExecutiveDeps(schedules *ScheduleResolver, targets *TargetsService) {
	if uc == nil {
		return
	}
	uc.schedules = schedules
	uc.targets = targets
}

func (uc *getOverviewUseCase) SetClock(clock func() time.Time) {
	if uc != nil && clock != nil {
		uc.now = clock
	}
}

func (uc *getOverviewUseCase) Execute(workspaceID string, filter attendance.OverviewFilter) (*attendance.Overview, error) {
	ctx := context.Background()

	summary, err := uc.Summary(ctx, workspaceID, filter)
	if err != nil {
		return nil, err
	}
	trend, err := uc.Trend(ctx, workspaceID, filter)
	if err != nil {
		return nil, err
	}
	team, err := uc.Team(ctx, workspaceID, filter)
	if err != nil {
		return nil, err
	}
	stages, err := uc.Stages(ctx, workspaceID, filter)
	if err != nil {
		return nil, err
	}
	backlog, err := uc.Backlog(ctx, workspaceID, filter)
	if err != nil {
		return nil, err
	}
	rework, err := uc.Rework(ctx, workspaceID, filter)
	if err != nil {
		return nil, err
	}

	return &attendance.Overview{
		SummarySection: *summary,
		TeamSection:    *team,
		TrendSection:   *trend,
		StagesSection:  *stages,
		BacklogSection: *backlog,
		ReworkSection:  *rework,
		LiveSection:    uc.liveSection(workspaceID, filter),
	}, nil
}

func (uc *getOverviewUseCase) clock() time.Time {
	if uc == nil || uc.now == nil {
		return nowUTC()
	}
	return uc.now()
}

func (uc *getOverviewUseCase) liveSection(workspaceID string, filter attendance.OverviewFilter) attendance.LiveSection {
	return attendance.LiveSection{
		Queue:     uc.queueStats(workspaceID, filter),
		Occupancy: uc.occupancy(workspaceID, filter),
		Live:      uc.livePresence(workspaceID),
	}
}

func (uc *getOverviewUseCase) queueStats(workspaceID string, filter attendance.OverviewFilter) attendance.OverviewQueue {
	if uc.queue == nil {
		return attendance.OverviewQueue{Available: false}
	}
	st, err := uc.queue.Stats(workspaceID, filter.DateFrom, filter.DateTo)
	if err != nil || st == nil {
		return attendance.OverviewQueue{Available: false}
	}
	out := attendance.OverviewQueue{
		Enqueued:    st.Enqueued,
		Connected:   st.Connected,
		Abandoned:   st.Abandoned,
		Overflow:    st.Overflow,
		QueueFull:   st.QueueFull,
		Cancelled:   st.Cancelled,
		AbandonRate: st.AbandonRate,
		Available:   st.Enqueued > 0 || st.Connected > 0 || st.Abandoned > 0,
	}
	if st.Connected > 0 && st.AvgASAMs > 0 {
		mins := math.Round((st.AvgASAMs/60000)*100) / 100
		out.AvgASAMins = &mins
	}
	return out
}

func (uc *getOverviewUseCase) occupancy(workspaceID string, filter attendance.OverviewFilter) attendance.OverviewOccupancy {
	if uc.presence == nil {
		return attendance.OverviewOccupancy{Available: false}
	}
	rows, err := uc.presence.Occupancy(workspaceID, filter.DateFrom, filter.DateTo)
	if err != nil || len(rows) == 0 {
		return attendance.OverviewOccupancy{Available: false}
	}
	var onlineMS, onCallMS int64
	var sumPct float64
	var n int64
	for _, row := range rows {
		onlineMS += row.OnlineMS
		onCallMS += row.OnCallMS
		if row.OnlineMS > 0 {
			sumPct += row.Occupancy
			n++
		}
	}
	out := attendance.OverviewOccupancy{
		AgentsSampled: int64(len(rows)),
		OnlineMS:      onlineMS,
		OnCallMS:      onCallMS,
		Available:     onlineMS > 0 || onCallMS > 0,
	}
	if n > 0 {
		avg := math.Round(sumPct/float64(n)*100) / 100
		out.AvgOccupancyPct = &avg
	}
	if onlineMS > 0 {
		team := math.Round(float64(onCallMS)/float64(onlineMS)*10000) / 100
		out.TeamOccupancyPct = &team
		idle := max(math.Round((100-team)*100)/100, 0)
		out.TeamIdlePct = &idle
	}
	return out
}

func (uc *getOverviewUseCase) livePresence(workspaceID string) attendance.OverviewLive {
	if uc.live == nil || workspaceID == "" {
		return attendance.OverviewLive{HasData: false, AsOf: uc.clock()}
	}
	rows := uc.live.ListPresence(workspaceID)
	live := attendance.OverviewLive{
		HasData: true,
		AsOf:    uc.clock(),
		Agents:  make([]attendance.OverviewLiveAgent, 0, len(rows)),
	}
	for _, p := range rows {
		live.Online++
		if p.Busy {
			live.InCall++
		} else {
			live.Free++
		}
		live.Agents = append(live.Agents, attendance.OverviewLiveAgent{
			UserID:     p.UserID,
			Busy:       p.Busy,
			HasBrowser: p.HasBrowser,
		})
	}
	if live.Online > 0 {
		idle := math.Round(float64(live.Free)/float64(live.Online)*10000) / 100
		busy := math.Round(float64(live.InCall)/float64(live.Online)*10000) / 100
		live.IdleRatePct = &idle
		live.BusyRatePct = &busy
	}
	return live
}
