package telephony_usecase

import (
	"math"
	"time"

	"vozko/domain/agent_presence"
	"vozko/domain/dialer"
	"vozko/domain/queue_event"
	"vozko/domain/telephony"
)

type getOverviewUseCase struct {
	repo     telephony.Repository
	queue    queue_event.Repository
	presence agent_presence.Repository
	live     dialer.DialerSessionRegistry
}

// NewGetOverviewUseCase builds a VoIP overview use case (extras optional).
func NewGetOverviewUseCase(repo telephony.Repository) telephony.GetOverviewUseCase {
	return &getOverviewUseCase{repo: repo}
}

// NewGetOverviewUseCaseWithDeps composes CDR aggregates with queue, presence, live dialer.
func NewGetOverviewUseCaseWithDeps(
	repo telephony.Repository,
	queue queue_event.Repository,
	presence agent_presence.Repository,
	live dialer.DialerSessionRegistry,
) telephony.GetOverviewUseCase {
	return &getOverviewUseCase{
		repo:     repo,
		queue:    queue,
		presence: presence,
		live:     live,
	}
}

func (uc *getOverviewUseCase) Execute(workspaceID string, filter telephony.OverviewFilter) (*telephony.Overview, error) {
	out, err := uc.repo.GetOverview(workspaceID, filter)
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = &telephony.Overview{Definitions: telephony.DefaultDefinitions(), SLAAvailable: true}
	}
	uc.fillQueue(workspaceID, filter, out)
	uc.fillOccupancy(workspaceID, filter, out)
	uc.fillLive(workspaceID, out)
	uc.enrichMembersWithOccupancy(workspaceID, filter, out)
	// CDR service level is computed in the repository; mark available when we have answered calls.
	if out.KPIs.Answered > 0 || out.KPIs.TotalCalls > 0 {
		out.SLAAvailable = true
	}
	return out, nil
}

// enrichMembersWithOccupancy merges presence occupancy into by_member rows.
func (uc *getOverviewUseCase) enrichMembersWithOccupancy(workspaceID string, filter telephony.OverviewFilter, out *telephony.Overview) {
	if uc.presence == nil || out == nil || len(out.ByMember) == 0 {
		return
	}
	rows, err := uc.presence.Occupancy(workspaceID, filter.DateFrom, filter.DateTo)
	if err != nil || len(rows) == 0 {
		return
	}
	byUser := make(map[string]agent_presence.OccupancyRow, len(rows))
	for _, r := range rows {
		byUser[r.UserID] = r
	}
	for i := range out.ByMember {
		pr, ok := byUser[out.ByMember[i].UserID]
		if !ok || pr.OnlineMS <= 0 {
			continue
		}
		occ := math.Round(pr.Occupancy*100) / 100
		out.ByMember[i].OccupancyPct = &occ
		idle := math.Round((100-occ)*100) / 100
		if idle < 0 {
			idle = 0
		}
		out.ByMember[i].IdlePct = &idle
	}
}

func (uc *getOverviewUseCase) fillQueue(workspaceID string, filter telephony.OverviewFilter, out *telephony.Overview) {
	if uc.queue == nil {
		out.Queue = telephony.QueueBlock{Available: false}
		return
	}
	slSec := filter.ServiceLevelSeconds
	if slSec <= 0 {
		slSec = telephony.DefaultServiceLevelSeconds
	}
	var st *queue_event.Stats
	var err error
	if withSL, ok := uc.queue.(interface {
		StatsWithSL(workspaceID string, from, to *time.Time, slSeconds int) (*queue_event.Stats, error)
	}); ok {
		st, err = withSL.StatsWithSL(workspaceID, filter.DateFrom, filter.DateTo, slSec)
	} else {
		st, err = uc.queue.Stats(workspaceID, filter.DateFrom, filter.DateTo)
	}
	if err != nil || st == nil {
		out.Queue = telephony.QueueBlock{Available: false}
		return
	}
	out.Queue = telephony.QueueBlock{
		Enqueued:            st.Enqueued,
		Connected:           st.Connected,
		Abandoned:           st.Abandoned,
		Overflow:            st.Overflow,
		QueueFull:           st.QueueFull,
		Cancelled:           st.Cancelled,
		AbandonRate:         st.AbandonRate,
		ServiceLevelSeconds: slSec,
		Available:           st.Enqueued > 0 || st.Connected > 0 || st.Abandoned > 0,
	}
	if st.Connected > 0 && st.AvgASAMs > 0 {
		mins := math.Round((st.AvgASAMs/60000)*100) / 100
		out.Queue.AvgASAMins = &mins
	}
	if st.MaxWaitMS > 0 {
		mins := math.Round((st.MaxWaitMS/60000)*100) / 100
		out.Queue.MaxWaitMins = &mins
	}
	if st.Connected > 0 {
		sl := st.ServiceLevelPct
		out.Queue.ServiceLevelPct = &sl
	}
}

func (uc *getOverviewUseCase) fillOccupancy(workspaceID string, filter telephony.OverviewFilter, out *telephony.Overview) {
	if uc.presence == nil {
		out.Occupancy = telephony.OccupancyBlock{Available: false}
		return
	}
	rows, err := uc.presence.Occupancy(workspaceID, filter.DateFrom, filter.DateTo)
	if err != nil || len(rows) == 0 {
		out.Occupancy = telephony.OccupancyBlock{Available: false}
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
	out.Occupancy = telephony.OccupancyBlock{
		AgentsSampled: int64(len(rows)),
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

func (uc *getOverviewUseCase) fillLive(workspaceID string, out *telephony.Overview) {
	if uc.live == nil || workspaceID == "" {
		out.Live = telephony.LiveBlock{HasData: false, AsOf: time.Now().UTC()}
		return
	}
	rows := uc.live.ListPresence(workspaceID)
	live := telephony.LiveBlock{
		HasData: true,
		AsOf:    time.Now().UTC(),
		Agents:  make([]telephony.LiveAgent, 0, len(rows)),
	}
	for _, p := range rows {
		live.Online++
		if p.Busy {
			live.InCall++
		} else {
			live.Free++
		}
		live.Agents = append(live.Agents, telephony.LiveAgent{
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
