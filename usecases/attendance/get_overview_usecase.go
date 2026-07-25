package attendance_usecase

import (
	"math"
	"time"

	"vozko/domain/agent_presence"
	"vozko/domain/attendance"
	"vozko/domain/dialer"
	"vozko/domain/queue_event"
)

// getOverviewUseCase composes the attendance repository overview with optional
// queue, presence, and live dialer ports. Keeps infra adapters out of the repository.
type getOverviewUseCase struct {
	repo     attendance.Repository
	queue    queue_event.Repository
	presence agent_presence.Repository
	// live is optional; when set, fills Overview.Live from dialer sessions.
	live dialer.DialerSessionRegistry
}

// NewGetOverviewUseCase builds the overview use case (queue/presence/live optional).
func NewGetOverviewUseCase(repo attendance.Repository) attendance.GetOverviewUseCase {
	return &getOverviewUseCase{repo: repo}
}

// NewGetOverviewUseCaseWithDeps injects queue ASA/abandon, occupancy, and live dialer.
func NewGetOverviewUseCaseWithDeps(
	repo attendance.Repository,
	queue queue_event.Repository,
	presence agent_presence.Repository,
	live dialer.DialerSessionRegistry,
) attendance.GetOverviewUseCase {
	return &getOverviewUseCase{
		repo:     repo,
		queue:    queue,
		presence: presence,
		live:     live,
	}
}

func (uc *getOverviewUseCase) Execute(workspaceID string, filter attendance.OverviewFilter) (*attendance.Overview, error) {
	out, err := uc.repo.GetOverview(workspaceID, filter)
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = &attendance.Overview{Definitions: attendance.DefaultDefinitions()}
	}

	uc.fillQueue(workspaceID, filter, out)
	uc.fillOccupancy(workspaceID, filter, out)
	uc.fillLive(workspaceID, out)
	return out, nil
}

func (uc *getOverviewUseCase) fillQueue(workspaceID string, filter attendance.OverviewFilter, out *attendance.Overview) {
	if uc.queue == nil {
		out.Queue = attendance.OverviewQueue{Available: false}
		return
	}
	st, err := uc.queue.Stats(workspaceID, filter.DateFrom, filter.DateTo)
	if err != nil || st == nil {
		out.Queue = attendance.OverviewQueue{Available: false}
		return
	}
	out.Queue = attendance.OverviewQueue{
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
		out.Queue.AvgASAMins = &mins
	}
}

func (uc *getOverviewUseCase) fillOccupancy(workspaceID string, filter attendance.OverviewFilter, out *attendance.Overview) {
	if uc.presence == nil {
		out.Occupancy = attendance.OverviewOccupancy{Available: false}
		return
	}
	rows, err := uc.presence.Occupancy(workspaceID, filter.DateFrom, filter.DateTo)
	if err != nil || len(rows) == 0 {
		out.Occupancy = attendance.OverviewOccupancy{Available: false}
		return
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
	out.Occupancy = attendance.OverviewOccupancy{
		AgentsSampled: int64(len(rows)),
		OnlineMS:      onlineMS,
		OnCallMS:      onCallMS,
		Available:     onlineMS > 0 || onCallMS > 0,
	}
	if n > 0 {
		avg := math.Round(sumPct/float64(n)*100) / 100
		out.Occupancy.AvgOccupancyPct = &avg
	}
	if onlineMS > 0 {
		team := math.Round(float64(onCallMS)/float64(onlineMS)*10000) / 100
		out.Occupancy.TeamOccupancyPct = &team
		idle := math.Round((100-team)*100) / 100
		if idle < 0 {
			idle = 0
		}
		out.Occupancy.TeamIdlePct = &idle
	}
}

func (uc *getOverviewUseCase) fillLive(workspaceID string, out *attendance.Overview) {
	if uc.live == nil || workspaceID == "" {
		out.Live = attendance.OverviewLive{HasData: false, AsOf: time.Now().UTC()}
		return
	}
	rows := uc.live.ListPresence(workspaceID)
	live := attendance.OverviewLive{
		HasData: true,
		AsOf:    time.Now().UTC(),
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
			HasBranch:  p.HasBranch,
		})
	}
	if live.Online > 0 {
		idle := math.Round(float64(live.Free)/float64(live.Online)*10000) / 100
		busy := math.Round(float64(live.InCall)/float64(live.Online)*10000) / 100
		live.IdleRatePct = &idle
		live.BusyRatePct = &busy
	}
	out.Live = live
}
