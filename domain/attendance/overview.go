package attendance

import "time"

type OverviewFilter struct {
	DateFrom     *time.Time `json:"date_from,omitempty"`
	DateTo       *time.Time `json:"date_to,omitempty"`
	DepartmentID string     `json:"department_id,omitempty"`
	MemberID     string     `json:"member_id,omitempty"`
	CampaignID   string     `json:"campaign_id,omitempty"`
	CampaignType string     `json:"campaign_type,omitempty"`
	Channel      string     `json:"channel,omitempty"`
	IncludeAI    bool       `json:"include_ai"`
	TrendBuckets int        `json:"trend_buckets,omitempty"`
	RankMetric   string     `json:"rank_metric,omitempty"`

	Quality QualityPolicy `json:"-"`
}

type OverviewKPIs struct {
	Engaged        int64 `json:"engaged"`
	ShellBacklog   int64 `json:"shell_backlog"`
	TotalScoped    int64 `json:"total_scoped"`
	EntriesCreated int64 `json:"entries_created"`

	Finished int64 `json:"finished"`
	Ongoing  int64 `json:"ongoing"`
	Pending  int64 `json:"pending"`

	NewLeads          int64 `json:"new_leads"`
	UnassignedBacklog int64 `json:"unassigned_backlog"`

	AvgHandleMins *float64 `json:"avg_handle_mins"`
	AvgWaitMins   *float64 `json:"avg_wait_mins"`
	AvgFRTMins    *float64 `json:"avg_frt_mins"`

	AvgRating            *float64 `json:"avg_rating"`
	CSATAvailable        bool     `json:"csat_available"`
	FRTSLAPercent        *float64 `json:"frt_sla_percent"`
	ResolutionSLAPercent *float64 `json:"resolution_sla_percent"`
	SLAAvailable         bool     `json:"sla_available"`
}

type HourlyPoint struct {
	Hour  int   `json:"hour"`
	Count int64 `json:"count"`
}

type StatusDistribution struct {
	Finished int64 `json:"finished"`
	Ongoing  int64 `json:"ongoing"`
	Pending  int64 `json:"pending"`
	Total    int64 `json:"total"`
}

type DepartmentRow struct {
	DepartmentID   string   `json:"department_id"`
	DepartmentName string   `json:"department_name"`
	AvgWaitMins    *float64 `json:"avg_wait_mins"`
	AvgHandleMins  *float64 `json:"avg_handle_mins"`
	Finished       int64    `json:"finished"`
	FinishedHuman  int64    `json:"finished_human"`
	FinishedAI     int64    `json:"finished_ai"`
	FinishedSystem int64    `json:"finished_system"`
	Ongoing        int64    `json:"ongoing"`
	Pending        int64    `json:"pending"`
}

type MemberRow struct {
	ActorID         string   `json:"actor_id"`
	ActorKind       string   `json:"actor_kind"`
	DisplayName     string   `json:"display_name"`
	Email           string   `json:"email,omitempty"`
	Presence        string   `json:"presence"`
	AvgResponseMins *float64 `json:"avg_response_mins"`
	Rating          *float64 `json:"rating"`
	ResolutionPct   float64  `json:"resolution_pct"`
	Open            int64    `json:"open"`
	Pending         int64    `json:"pending"`
	Resolved        int64    `json:"resolved"`
	FinishedHuman   int64    `json:"finished_human"`
	FinishedAI      int64    `json:"finished_ai"`
	FinishedSystem  int64    `json:"finished_system"`

	TotalMessages   int64    `json:"total_messages"`
	InboundMessages int64    `json:"inbound_messages"`
	AvgMessages     *float64 `json:"avg_messages"`
}

type OverviewFRT struct {
	AvgMins      *float64 `json:"avg_mins"`
	MedianMins   *float64 `json:"median_mins"`
	HumanAvgMins *float64 `json:"human_avg_mins"`
	AIAvgMins    *float64 `json:"ai_avg_mins"`
	SampleCount  int64    `json:"sample_count"`
	HumanSamples int64    `json:"human_samples"`
	AISamples    int64    `json:"ai_samples"`
	Available    bool     `json:"available"`
}

type OverviewAI struct {
	Sessions        int64   `json:"sessions"`
	Contained       int64   `json:"contained"`
	HandedOff       int64   `json:"handed_off"`
	Abandoned       int64   `json:"abandoned"`
	OpenSessions    int64   `json:"open_sessions"`
	ContainmentRate float64 `json:"containment_rate"`
	HandoffRate     float64 `json:"handoff_rate"`
	AvgAIMessages   float64 `json:"avg_ai_messages"`
	Available       bool    `json:"available"`
}

type OverviewQueue struct {
	Enqueued    int64    `json:"enqueued"`
	Connected   int64    `json:"connected"`
	Abandoned   int64    `json:"abandoned"`
	Overflow    int64    `json:"overflow"`
	QueueFull   int64    `json:"queue_full"`
	Cancelled   int64    `json:"cancelled"`
	AvgASAMins  *float64 `json:"avg_asa_mins"`
	AbandonRate float64  `json:"abandon_rate"`
	Available   bool     `json:"available"`
}

type OverviewOccupancy struct {
	AvgOccupancyPct  *float64 `json:"avg_occupancy_pct"`
	AgentsSampled    int64    `json:"agents_sampled"`
	OnlineMS         int64    `json:"online_ms"`
	OnCallMS         int64    `json:"on_call_ms"`
	TeamOccupancyPct *float64 `json:"team_occupancy_pct"`
	TeamIdlePct      *float64 `json:"team_idle_pct"`
	Available        bool     `json:"available"`
}

type OverviewLive struct {
	Online      int64               `json:"online"`
	InCall      int64               `json:"in_call"`
	Free        int64               `json:"free"`
	IdleRatePct *float64            `json:"idle_rate_pct"`
	BusyRatePct *float64            `json:"busy_rate_pct"`
	Agents      []OverviewLiveAgent `json:"agents,omitempty"`
	HasData     bool                `json:"has_data"`
	AsOf        time.Time           `json:"as_of"`
}

type OverviewLiveAgent struct {
	UserID     string `json:"user_id"`
	Busy       bool   `json:"busy"`
	HasBrowser bool   `json:"has_browser"`
}

type ChannelSlice struct {
	Channel string  `json:"channel"`
	Count   int64   `json:"count"`
	Pct     float64 `json:"pct"`
}

type OverviewMessaging struct {
	AvgMessagesPerConversation *float64 `json:"avg_messages_per_conversation"`
	AvgInbound                 *float64 `json:"avg_inbound"`
	AvgOutbound                *float64 `json:"avg_outbound"`
	AvgTemplate                *float64 `json:"avg_template,omitempty"`
	TemplateMessages           int64    `json:"template_messages"`
	ConversationsWithMessages  int64    `json:"conversations_with_messages"`
	ConversationsWithTemplate  int64    `json:"conversations_with_template"`
	AvgMessagesAllScoped       *float64 `json:"avg_messages_all_scoped,omitempty"`
	Available                  bool     `json:"available"`
}

type OverviewReopen struct {
	ReopenedCount      int64    `json:"reopened_count"`
	FinishedCount      int64    `json:"finished_count"`
	FinishedEventCount int64    `json:"finished_event_count"`
	ReopenRate         *float64 `json:"reopen_rate"`
	Available          bool     `json:"available"`
}

type OverviewFinishedBySource struct {
	Human     int64    `json:"human"`
	AI        int64    `json:"ai"`
	System    int64    `json:"system"`
	Total     int64    `json:"total"`
	HumanPct  *float64 `json:"human_pct,omitempty"`
	AIPct     *float64 `json:"ai_pct,omitempty"`
	SystemPct *float64 `json:"system_pct,omitempty"`
	Available bool     `json:"available"`
}

type MetricDefinitions struct {
	PeriodScope      string `json:"period_scope"`
	Engaged          string `json:"engaged"`
	Shell            string `json:"shell"`
	StatusMapping    string `json:"status_mapping"`
	WaitTime         string `json:"wait_time"`
	HandleTime       string `json:"handle_time"`
	Resolution       string `json:"resolution"`
	FRT              string `json:"frt"`
	AI               string `json:"ai"`
	Queue            string `json:"queue"`
	Occupancy        string `json:"occupancy"`
	ChannelMix       string `json:"channel_mix"`
	NewLeads         string `json:"new_leads"`
	Messaging        string `json:"messaging"`
	Reopen           string `json:"reopen"`
	FinishedBySource string `json:"finished_by_source"`
	Stages           string `json:"stages"`
	Unassigned       string `json:"unassigned"`
	Period           string `json:"period"`
	Projection       string `json:"projection"`
	Standing         string `json:"standing"`
	Trend            string `json:"trend"`
	Revenue          string `json:"revenue"`
	BacklogXray      string `json:"backlog_xray"`
	Quality          string `json:"quality"`
	TeamRanking      string `json:"team_ranking"`
	Rework           string `json:"rework"`
	CSAT             string `json:"csat"`
	SLA              string `json:"sla"`
}

type Overview struct {
	Filter             OverviewFilter     `json:"filter"`
	KPIs               OverviewKPIs       `json:"kpis"`
	Hourly             []HourlyPoint      `json:"hourly"`
	StatusDistribution StatusDistribution `json:"status_distribution"`
	ByDepartment       []DepartmentRow    `json:"by_department"`
	ByMember           []MemberRow        `json:"by_member"`

	FRT              OverviewFRT              `json:"frt"`
	AI               OverviewAI               `json:"ai"`
	Queue            OverviewQueue            `json:"queue"`
	Occupancy        OverviewOccupancy        `json:"occupancy"`
	Live             OverviewLive             `json:"live"`
	ChannelMix       []ChannelSlice           `json:"channel_mix"`
	Messaging        OverviewMessaging        `json:"messaging"`
	Reopen           OverviewReopen           `json:"reopen"`
	FinishedBySource OverviewFinishedBySource `json:"finished_by_source"`
	Stages           OverviewStages           `json:"stages"`

	Period      Period             `json:"period"`
	Projections []MetricProjection `json:"projections"`
	Standing    Standing           `json:"standing"`
	Trend       Trend              `json:"trend"`
	Revenue     Revenue            `json:"revenue"`
	BacklogXray BacklogXray        `json:"backlog_xray"`
	Quality     Quality            `json:"quality"`
	TeamRanking TeamRanking        `json:"team_ranking"`
	Rework      OverviewRework     `json:"rework"`

	GeneratedAt time.Time `json:"generated_at"`

	Definitions MetricDefinitions `json:"definitions"`
}

func DefaultDefinitions() MetricDefinitions {
	return MetricDefinitions{
		PeriodScope:      "raw scope: entry created in range OR has message in range",
		Engaged:          "scoped entry with ≥1 non-deleted message; primary attendance KPIs use this set",
		Shell:            "scoped entry with 0 messages (campaign/API shell); reported as shell_backlog only",
		StatusMapping:    "finished=finished; ongoing=ongoing; pending=new|empty (engaged only in KPIs)",
		WaitTime:         "first user_message|audio|media → first operator|ai_response (mins)",
		HandleTime:       "first operator|ai_response → last agent message (finished only)",
		Resolution:       "resolved/(open+pending+resolved)*100 per assignee (engaged)",
		FRT:              "assignment_history.started_at → first operator|ai_response after start",
		AI:               "ended ai_attendance_sessions: containment=contained/ended, handoff=handed_off/ended",
		Queue:            "queue_events: ASA=avg waited_ms on connected; abandon=abandoned/enqueued",
		Occupancy:        "on_call_ms / (online+on_call)_ms; idle=100-occupancy (historical ociosidade)",
		ChannelMix:       "engaged entries grouped by entry_type (whatsapp|voice)",
		NewLeads:         "leads created in range (leads.created_at, workspace-wide); date filter only, not narrowed by department/member/channel/campaign",
		Messaging:        "avg messages per engaged entry; avg_messages_all_scoped includes shells",
		Reopen:           "conversation_events reopened / engaged finished (KPI); finished_event_count is telemetry",
		FinishedBySource: "engaged finished by close_source: human (incl empty/legacy), ai, system",
		Stages:           "scoped conversations by their current stage, grouped by owning funnel; engaged is the headline and shells are reported beside it; dwell and stuck are measured over OPEN engaged rows against now",
		Unassigned:       "engaged entries with empty assignee and status != finished",
		Period:           "calendar month containing date_to, measured in OPEN business days from the resolved working-hours schedule (department overrides workspace); unavailable when no schedule is configured",
		Projection:       "cumulative metrics only: actual / open_days_done * open_days_total. Averages and rates are never projected. Needs >= 1 open day done and >= 10% of the period elapsed",
		Standing:         "share of configured targets whose projection is on track, mapped to a cluster band; unavailable when no target is set",
		Trend:            "closed calendar months bucketed on closed_at (finished) or created_at (created); best value is the best CLOSED bucket inside the returned window, never an all-time record",
		Revenue:          "won opportunities attributed by close_date to the owner in owner_id; never summed across currencies; unavailable when a department filter is set because opportunities carry no department",
		BacklogXray:      "engaged entries with status != finished, broken down by origin, assignee, age, lead tenure, returning contact, required-field fill and per-channel 24h window reachability; every percentage is over its own measured population",
		Quality:          "durable outcomes over closes subject to outcome capture, attributed to the assignee; the denominator starts at outcome_capture.enabled_at and excludes reserved system, AI and workflow codes",
		Rework:           "engaged finished conversations that were reopened afterwards, attributed to the assignee; cost is the WhatsApp templates charged on those conversations AFTER the reopen, in the workspace billing currency",
		TeamRanking:      "members ranked by the declared metric; the team average is over HUMAN actors only, AI and system actors are reported separately with no class; a class needs at least 20 closes",
		CSAT:             "not available (csat_available=false) until surveys ship",
		SLA:              "not available (sla_available=false) until SLA policies ship",
	}
}

type GetOverviewUseCase interface {
	Execute(workspaceID string, filter OverviewFilter) (*Overview, error)
}
