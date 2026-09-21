package telephony

import "time"

const DefaultServiceLevelSeconds = 20

type OverviewFilter struct {
	DateFrom            *time.Time `json:"date_from,omitempty"`
	DateTo              *time.Time `json:"date_to,omitempty"`
	Direction           string     `json:"direction,omitempty"`
	CallType            string     `json:"call_type,omitempty"`
	AgentID             string     `json:"agent_id,omitempty"`
	MemberID            string     `json:"member_id,omitempty"`
	ServiceLevelSeconds int        `json:"service_level_seconds,omitempty"`
}

type OverviewKPIs struct {
	TotalCalls          int64    `json:"total_calls"`
	Answered            int64    `json:"answered"`
	Failed              int64    `json:"failed"`
	Abandoned           int64    `json:"abandoned"`
	ConnectRate         *float64 `json:"connect_rate"`
	Inbound             int64    `json:"inbound"`
	Outbound            int64    `json:"outbound"`
	AvgRingMins         *float64 `json:"avg_ring_mins"`
	AvgTalkMins         *float64 `json:"avg_talk_mins"`
	AvgHandleMins       *float64 `json:"avg_handle_mins"`
	AvgAHTMins          *float64 `json:"avg_aht_mins"`
	AvgHoldMins         *float64 `json:"avg_hold_mins"`
	AvgACWMins          *float64 `json:"avg_acw_mins"`
	HumanCRMCalls       int64    `json:"human_crm_calls"`
	TrunkInbound        int64    `json:"trunk_inbound"`
	TrunkOutbound       int64    `json:"trunk_outbound"`
	ServiceLevelPct     *float64 `json:"service_level_pct"`
	ServiceLevelSeconds int      `json:"service_level_seconds"`
	AnsweredWithinSL    int64    `json:"answered_within_sl"`
	CDRAbandonRate      *float64 `json:"cdr_abandon_rate"`
	ShortAbandons       int64    `json:"short_abandons"`
	Transfers           int64    `json:"transfers"`
}

type HourlyPoint struct {
	Hour  int   `json:"hour"`
	Count int64 `json:"count"`
}

type TypeSlice struct {
	Type  string  `json:"type"`
	Count int64   `json:"count"`
	Pct   float64 `json:"pct"`
}

type DirectionSlice struct {
	Direction string  `json:"direction"`
	Count     int64   `json:"count"`
	Pct       float64 `json:"pct"`
}

type DispositionSlice struct {
	Code  string  `json:"code"`
	Label string  `json:"label,omitempty"`
	Count int64   `json:"count"`
	Pct   float64 `json:"pct"`
}

type QueueBlock struct {
	Enqueued            int64    `json:"enqueued"`
	Connected           int64    `json:"connected"`
	Abandoned           int64    `json:"abandoned"`
	Overflow            int64    `json:"overflow"`
	QueueFull           int64    `json:"queue_full"`
	Cancelled           int64    `json:"cancelled"`
	AvgASAMins          *float64 `json:"avg_asa_mins"`
	MaxWaitMins         *float64 `json:"max_wait_mins"`
	ServiceLevelPct     *float64 `json:"service_level_pct"`
	ServiceLevelSeconds int      `json:"service_level_seconds"`
	AbandonRate         float64  `json:"abandon_rate"`
	Available           bool     `json:"available"`
}

type OccupancyBlock struct {
	AvgOccupancyPct  *float64 `json:"avg_occupancy_pct"`
	TeamOccupancyPct *float64 `json:"team_occupancy_pct"`
	TeamIdlePct      *float64 `json:"team_idle_pct"`
	AgentsSampled    int64    `json:"agents_sampled"`
	Available        bool     `json:"available"`
}

type LiveBlock struct {
	Online      int64       `json:"online"`
	InCall      int64       `json:"in_call"`
	Free        int64       `json:"free"`
	IdleRatePct *float64    `json:"idle_rate_pct"`
	BusyRatePct *float64    `json:"busy_rate_pct"`
	Agents      []LiveAgent `json:"agents,omitempty"`
	HasData     bool        `json:"has_data"`
	AsOf        time.Time   `json:"as_of"`
}

type LiveAgent struct {
	UserID     string `json:"user_id"`
	Busy       bool   `json:"busy"`
	HasBrowser bool   `json:"has_browser"`
}

type MemberRow struct {
	UserID        string   `json:"user_id"`
	TotalCalls    int64    `json:"total_calls"`
	Answered      int64    `json:"answered"`
	Failed        int64    `json:"failed"`
	Abandoned     int64    `json:"abandoned"`
	ConnectRate   float64  `json:"connect_rate"`
	AvgTalkMins   *float64 `json:"avg_talk_mins"`
	AvgRingMins   *float64 `json:"avg_ring_mins"`
	AvgHandleMins *float64 `json:"avg_handle_mins"`
	WithinSL      int64    `json:"within_sl"`
	ServiceLevel  *float64 `json:"service_level_pct,omitempty"`
	OccupancyPct  *float64 `json:"occupancy_pct,omitempty"`
	IdlePct       *float64 `json:"idle_pct,omitempty"`
}

type MetricDefinitions struct {
	ConnectRate  string `json:"connect_rate"`
	RingTime     string `json:"ring_time"`
	TalkTime     string `json:"talk_time"`
	AHT          string `json:"aht"`
	ASA          string `json:"asa"`
	ServiceLevel string `json:"service_level"`
	Occupancy    string `json:"occupancy"`
	Idle         string `json:"idle"`
	Disposition  string `json:"disposition"`
	ByMember     string `json:"by_member"`
	Persistence  string `json:"persistence"`
	Gaps         string `json:"gaps"`
}

type Overview struct {
	Filter       OverviewFilter     `json:"filter"`
	KPIs         OverviewKPIs       `json:"kpis"`
	Hourly       []HourlyPoint      `json:"hourly"`
	ByType       []TypeSlice        `json:"by_type"`
	ByDirection  []DirectionSlice   `json:"by_direction"`
	Dispositions []DispositionSlice `json:"dispositions"`
	Queue        QueueBlock         `json:"queue"`
	Occupancy    OccupancyBlock     `json:"occupancy"`
	Live         LiveBlock          `json:"live"`
	ByMember     []MemberRow        `json:"by_member"`
	SLAAvailable bool               `json:"sla_available"`
	Definitions  MetricDefinitions  `json:"definitions"`
}

func DefaultDefinitions() MetricDefinitions {
	return MetricDefinitions{
		ConnectRate:  "answered calls / total attempts in range * 100",
		RingTime:     "answered_at - started_at (mins) for answered calls",
		TalkTime:     "ended_at - answered_at (mins) when both set",
		AHT:          "talk + hold_sec + acw_sec when set; else duration_sec for answered (industry AHT approx)",
		ASA:          "avg queue_events.waited_ms for type=connected (mins)",
		ServiceLevel: "answered with ring <= 20s (default) / answered * 100, industry 80/20",
		Occupancy:    "on_call_ms / online_ms from agent_presence_intervals",
		Idle:         "live: free/online; historical: 100 - occupancy",
		Disposition:  "calls.status + end_reason stacks",
		ByMember:     "human CDR agent_id (call session user) grouped: volume, connect, times, SL, occupancy",
		Persistence:  "all metrics from durable tables except live call session registry snapshot",
		Gaps:         "true hold/ACW capture on hot path, RPC disposition product codes, schedule adherence, see docs/VOIP_METRICS.md",
	}
}

type Repository interface {
	GetOverview(workspaceID string, filter OverviewFilter) (*Overview, error)
}

type GetOverviewUseCase interface {
	Execute(workspaceID string, filter OverviewFilter) (*Overview, error)
}
