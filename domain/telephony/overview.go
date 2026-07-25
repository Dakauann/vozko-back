package telephony

import "time"

// DefaultServiceLevelSeconds is the industry-classic 80/20 threshold (20 seconds).
const DefaultServiceLevelSeconds = 20

// OverviewFilter scopes the VoIP / dialer dashboard.
type OverviewFilter struct {
	DateFrom  *time.Time `json:"date_from,omitempty"`
	DateTo    *time.Time `json:"date_to,omitempty"`
	Direction string     `json:"direction,omitempty"` // inbound | outbound | ""
	CallType  string     `json:"call_type,omitempty"` // crm | trunk_inbound | trunk_outbound | ""
	AgentID   string     `json:"agent_id,omitempty"`  // dialer member uuid on CDR
	MemberID  string     `json:"member_id,omitempty"` // human dialer user (same column as agent_id on CRM)
	// ServiceLevelSeconds overrides DefaultServiceLevelSeconds when > 0.
	ServiceLevelSeconds int `json:"service_level_seconds,omitempty"`
}

// OverviewKPIs is the primary VoIP strip (industry names mapped to PT-friendly UI).
type OverviewKPIs struct {
	TotalCalls    int64    `json:"total_calls"`
	Answered      int64    `json:"answered"`
	Failed        int64    `json:"failed"`
	Abandoned     int64    `json:"abandoned"`
	ConnectRate   *float64 `json:"connect_rate"` // answered/total * 100
	Inbound       int64    `json:"inbound"`
	Outbound      int64    `json:"outbound"`
	AvgRingMins   *float64 `json:"avg_ring_mins"`   // started → answered
	AvgTalkMins   *float64 `json:"avg_talk_mins"`   // answered → ended
	AvgHandleMins *float64 `json:"avg_handle_mins"` // duration_sec avg on answered
	// AvgAHTMins is industry AHT approximation: talk + hold + acw when columns set;
	// falls back to duration_sec / talk when hold/acw unavailable.
	AvgAHTMins    *float64 `json:"avg_aht_mins"`
	AvgHoldMins   *float64 `json:"avg_hold_mins"`
	AvgACWMins    *float64 `json:"avg_acw_mins"`
	HumanCRMCalls int64    `json:"human_crm_calls"`
	TrunkInbound  int64    `json:"trunk_inbound"`
	TrunkOutbound int64    `json:"trunk_outbound"`
	// Service level (industry 80/20): answered within threshold / offered answered attempts.
	ServiceLevelPct     *float64 `json:"service_level_pct"`
	ServiceLevelSeconds int      `json:"service_level_seconds"`
	AnsweredWithinSL    int64    `json:"answered_within_sl"`
	// CDR abandon rate among total attempts.
	CDRAbandonRate *float64 `json:"cdr_abandon_rate"`
	// Short hangups (<6s after answer) — early customer drop signal.
	ShortAbandons int64 `json:"short_abandons"`
	// Transfers completed (conversation_events) in range.
	Transfers int64 `json:"transfers"`
}

// HourlyPoint is call starts per hour of day.
type HourlyPoint struct {
	Hour  int   `json:"hour"`
	Count int64 `json:"count"`
}

// TypeSlice is volume by CDR type.
type TypeSlice struct {
	Type  string  `json:"type"`
	Count int64   `json:"count"`
	Pct   float64 `json:"pct"`
}

// DirectionSlice is inbound vs outbound share.
type DirectionSlice struct {
	Direction string  `json:"direction"`
	Count     int64   `json:"count"`
	Pct       float64 `json:"pct"`
}

// DispositionSlice is outcome stack (status + end_reason).
type DispositionSlice struct {
	Code  string  `json:"code"`
	Label string  `json:"label,omitempty"`
	Count int64   `json:"count"`
	Pct   float64 `json:"pct"`
}

// QueueBlock is ACD queue KPIs.
type QueueBlock struct {
	Enqueued    int64    `json:"enqueued"`
	Connected   int64    `json:"connected"`
	Abandoned   int64    `json:"abandoned"`
	Overflow    int64    `json:"overflow"`
	QueueFull   int64    `json:"queue_full"`
	Cancelled   int64    `json:"cancelled"`
	AvgASAMins  *float64 `json:"avg_asa_mins"`
	MaxWaitMins *float64 `json:"max_wait_mins"`
	// ServiceLevelPct: connected with waited_ms <= threshold / connected * 100.
	ServiceLevelPct     *float64 `json:"service_level_pct"`
	ServiceLevelSeconds int      `json:"service_level_seconds"`
	AbandonRate         float64  `json:"abandon_rate"`
	Available           bool     `json:"available"`
}

// OccupancyBlock is historical presence occupancy / idle.
type OccupancyBlock struct {
	AvgOccupancyPct  *float64 `json:"avg_occupancy_pct"`
	TeamOccupancyPct *float64 `json:"team_occupancy_pct"`
	TeamIdlePct      *float64 `json:"team_idle_pct"`
	AgentsSampled    int64    `json:"agents_sampled"`
	Available        bool     `json:"available"`
}

// LiveBlock is point-in-time dialer presence.
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

// LiveAgent is one online dialer contact.
type LiveAgent struct {
	UserID     string `json:"user_id"`
	Busy       bool   `json:"busy"`
	HasBrowser bool   `json:"has_browser"`
	HasBranch  bool   `json:"has_branch"`
}

// MemberRow is per human agent VoIP performance (CDR agent_id = user id on CRM/trunk).
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
	// WithinSL: answered with ring <= service level seconds.
	WithinSL     int64    `json:"within_sl"`
	ServiceLevel *float64 `json:"service_level_pct,omitempty"`
	OccupancyPct *float64 `json:"occupancy_pct,omitempty"`
	IdlePct      *float64 `json:"idle_pct,omitempty"`
}

// MetricDefinitions documents contracts for clients.
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

// Overview is the VoIP / dialer dashboard payload.
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
	// SLAAvailable is true when Service Level is computed from CDR ring times
	// (industry metric available). Formal multi-policy SLA product remains separate.
	SLAAvailable bool              `json:"sla_available"`
	Definitions  MetricDefinitions `json:"definitions"`
}

// DefaultDefinitions freezes product contracts.
func DefaultDefinitions() MetricDefinitions {
	return MetricDefinitions{
		ConnectRate:  "answered calls / total attempts in range * 100",
		RingTime:     "answered_at - started_at (mins) for answered calls",
		TalkTime:     "ended_at - answered_at (mins) when both set",
		AHT:          "talk + hold_sec + acw_sec when set; else duration_sec for answered (industry AHT approx)",
		ASA:          "avg queue_events.waited_ms for type=connected (mins)",
		ServiceLevel: "answered with ring <= 20s (default) / answered * 100 — industry 80/20",
		Occupancy:    "on_call_ms / online_ms from agent_presence_intervals",
		Idle:         "live: free/online; historical: 100 - occupancy",
		Disposition:  "calls.status + end_reason stacks",
		ByMember:     "human CDR agent_id (dialer user) grouped: volume, connect, times, SL, occupancy",
		Persistence:  "all metrics from durable tables except live dialer registry snapshot",
		Gaps:         "true hold/ACW capture on hot path, RPC disposition product codes, schedule adherence — see docs/VOIP_METRICS.md",
	}
}

// Repository is the CDR aggregation port for telephony overview.
type Repository interface {
	GetOverview(workspaceID string, filter OverviewFilter) (*Overview, error)
}

// GetOverviewUseCase is the single entry for the VoIP dashboard.
type GetOverviewUseCase interface {
	Execute(workspaceID string, filter OverviewFilter) (*Overview, error)
}
