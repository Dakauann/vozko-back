package attendance

import "time"

// ── Metric definitions (frozen product contracts) ───────────────────────────
//
// Period scope (raw): an entry is IN the report universe if it was created in
// [from,to] OR it has at least one non-deleted message in [from,to].
//
// Engaged vs shell (attendance semantics):
//   engaged = scoped entry with total non-deleted messages > 0  (a real thread)
//   shell   = scoped entry with zero messages (campaign/API create, never started)
//
// Primary KPIs, status distribution, channel mix, hourly volume, department
// volumes, member loads, unassigned backlog, finished-by-source, and messaging
// averages count ENGAGED only. Shells are reported separately so bulk campaign
// drops do not look like unanswered live chats.
//
// Status buckets (conversation_status on campaign entries):
//   finished = "finished"
//   ongoing  = "ongoing"
//   pending  = "new" OR empty string (waiting for first agent/AI engagement)
//
// Wait time (chat/voice messaging): seconds from first inbound user_message
// in the period scope to the first operator OR ai_response after that message.
//
// Handle time: seconds from first agent-side message (operator|ai_response) to
// the later of (last agent-side message, entry updated_at when status=finished).
// Only computed for finished entries to avoid open-ended timers.
//
// Resolution rate (member): resolved / (open + pending + resolved) * 100
// among engaged assigned entries.
//
// FRT (assignment-aware): assignment_history.started_at → first operator|ai_response
// after ownership start.
//
// AI containment / handoff: ai_attendance_sessions.outcome on ended sessions.
//
// Queue ASA: avg waited_ms on queue_events type=connected.
// Abandon rate: abandoned / enqueued on queue_events.
//
// Occupancy: on_call_ms / (online_ms + on_call_ms) from agent_presence_intervals.
//
// Channel mix: engaged entries by entry_type (whatsapp | voice).
// Unassigned backlog: engaged entries with no assignee and not finished.
// Messages per conversation: avg non-deleted messages per ENGAGED entry.
// (Optional dual: avg over all scoped entries is avg_messages_all_scoped.)
// Reopen rate: conversation_events reopened in range / engaged finished (KPI).
// Finished event count is kept for telemetry only (may include historic shells).
//
// Finished by close_source (engaged finished entries):
//   human  = close_source = 'human' OR empty/null (legacy hand closes, backfilled)
//   ai     = close_source = 'ai'
//   system = close_source = 'system' (idle auto-close / customer_idle)
// Total of the three equals KPIs.Finished for the same engaged set.
//
// CSAT / Avaliação: not available until survey product exists (csat_available=false).
// SLA: not available until SLA policies ship (sla_available=false).
//
// AI agents appear as team rows with actor_kind=ai when include_ai is true.

// OverviewFilter drives every widget in the ops overview.
type OverviewFilter struct {
	DateFrom     *time.Time `json:"date_from,omitempty"`
	DateTo       *time.Time `json:"date_to,omitempty"`
	DepartmentID string     `json:"department_id,omitempty"`
	MemberID     string     `json:"member_id,omitempty"` // human user id
	CampaignID   string     `json:"campaign_id,omitempty"`
	CampaignType string     `json:"campaign_type,omitempty"` // whatsapp | voice | ""
	Channel      string     `json:"channel,omitempty"`       // whatsapp | voice | support | ""
	IncludeAI    bool       `json:"include_ai"`
}

// OverviewKPIs maps 1:1 to the primary KPI strip (engaged conversations).
type OverviewKPIs struct {
	// Engaged: scoped entries with at least one non-deleted message.
	Engaged int64 `json:"engaged"`
	// ShellBacklog: scoped entries with zero messages (campaign shells).
	ShellBacklog int64 `json:"shell_backlog"`
	// TotalScoped: engaged + shell (raw period universe).
	TotalScoped int64 `json:"total_scoped"`
	// EntriesCreated: entries created in range (is_new_contact), including shells.
	EntriesCreated int64 `json:"entries_created"`

	Finished    int64 `json:"finished"`     // engaged only
	Ongoing     int64 `json:"ongoing"`      // engaged only
	Pending     int64 `json:"pending"`      // engaged only
	NewContacts int64 `json:"new_contacts"` // engaged + created in range
	// UnassignedBacklog: engaged, no inbox assignee, not finished.
	UnassignedBacklog int64 `json:"unassigned_backlog"`

	AvgHandleMins *float64 `json:"avg_handle_mins"` // null when no samples
	AvgWaitMins   *float64 `json:"avg_wait_mins"`
	// AvgFRTMins is assignment-aware first response (null when no samples).
	AvgFRTMins *float64 `json:"avg_frt_mins"`

	AvgRating     *float64 `json:"avg_rating"` // always null until CSAT
	CSATAvailable bool     `json:"csat_available"`
	// SLA placeholders (null until SLA policies exist)
	FRTSLAPercent        *float64 `json:"frt_sla_percent"`
	ResolutionSLAPercent *float64 `json:"resolution_sla_percent"`
	SLAAvailable         bool     `json:"sla_available"`
}

// HourlyPoint is engaged conversations per hour of day (0-23) by entry.created_at hour.
type HourlyPoint struct {
	Hour  int   `json:"hour"`
	Count int64 `json:"count"`
}

// StatusDistribution powers the donut chart (engaged only).
type StatusDistribution struct {
	Finished int64 `json:"finished"`
	Ongoing  int64 `json:"ongoing"`
	Pending  int64 `json:"pending"`
	Total    int64 `json:"total"`
}

// DepartmentRow is performance by department (engaged entries only).
type DepartmentRow struct {
	DepartmentID   string   `json:"department_id"`
	DepartmentName string   `json:"department_name"`
	AvgWaitMins    *float64 `json:"avg_wait_mins"`
	AvgHandleMins  *float64 `json:"avg_handle_mins"`
	Finished       int64    `json:"finished"`
	// Finished split by close_source (same rules as OverviewFinishedBySource).
	// Gamification: intentional (human+ai) vs silence (system), not queue abandon.
	FinishedHuman  int64 `json:"finished_human"`
	FinishedAI     int64 `json:"finished_ai"`
	FinishedSystem int64 `json:"finished_system"`
	Ongoing        int64 `json:"ongoing"`
	Pending        int64 `json:"pending"`
}

// MemberRow is the team table (human or AI) over engaged assigned entries.
type MemberRow struct {
	ActorID         string   `json:"actor_id"`
	ActorKind       string   `json:"actor_kind"` // human | ai
	DisplayName     string   `json:"display_name"`
	Email           string   `json:"email,omitempty"`
	Presence        string   `json:"presence"` // online | offline | on_call | unknown
	AvgResponseMins *float64 `json:"avg_response_mins"`
	Rating          *float64 `json:"rating"` // null until CSAT
	ResolutionPct   float64  `json:"resolution_pct"`
	Open            int64    `json:"open"`     // ongoing
	Pending         int64    `json:"pending"`  // new/empty
	Resolved        int64    `json:"resolved"` // finished (all close sources)
	// How assigned finished chats were closed (for fair ranking vs silence auto-close).
	FinishedHuman  int64 `json:"finished_human"`
	FinishedAI     int64 `json:"finished_ai"`
	FinishedSystem int64 `json:"finished_system"`
}

// OverviewFRT is assignment-aware first response time.
type OverviewFRT struct {
	AvgMins      *float64 `json:"avg_mins"`
	MedianMins   *float64 `json:"median_mins"`
	HumanAvgMins *float64 `json:"human_avg_mins"`
	AIAvgMins    *float64 `json:"ai_avg_mins"`
	SampleCount  int64    `json:"sample_count"`
	HumanSamples int64    `json:"human_samples"`
	AISamples    int64    `json:"ai_samples"`
	Available    bool     `json:"available"` // true when sample_count > 0
}

// OverviewAI is workspace-level AI session outcomes.
type OverviewAI struct {
	Sessions        int64   `json:"sessions"`
	Contained       int64   `json:"contained"`
	HandedOff       int64   `json:"handed_off"`
	Abandoned       int64   `json:"abandoned"`
	OpenSessions    int64   `json:"open_sessions"`
	ContainmentRate float64 `json:"containment_rate"`
	HandoffRate     float64 `json:"handoff_rate"`
	AvgAIMessages   float64 `json:"avg_ai_messages"`
	Available       bool    `json:"available"` // true when any session in range
}

// OverviewQueue is dialer ACD queue KPIs from queue_events.
type OverviewQueue struct {
	Enqueued    int64    `json:"enqueued"`
	Connected   int64    `json:"connected"`
	Abandoned   int64    `json:"abandoned"`
	Overflow    int64    `json:"overflow"`
	QueueFull   int64    `json:"queue_full"`
	Cancelled   int64    `json:"cancelled"`
	AvgASAMins  *float64 `json:"avg_asa_mins"` // ASA in minutes
	AbandonRate float64  `json:"abandon_rate"` // percent
	Available   bool     `json:"available"`    // true when enqueued > 0
}

// OverviewOccupancy aggregates presence intervals across the team.
type OverviewOccupancy struct {
	AvgOccupancyPct *float64 `json:"avg_occupancy_pct"` // mean of per-agent occupancy
	AgentsSampled   int64    `json:"agents_sampled"`
	OnlineMS        int64    `json:"online_ms"`
	OnCallMS        int64    `json:"on_call_ms"`
	// TeamOccupancyPct: OnCallMS / OnlineMS (online already includes on_call in source)
	TeamOccupancyPct *float64 `json:"team_occupancy_pct"`
	// TeamIdlePct is ociosidade histórica: 100 - TeamOccupancyPct (when available).
	TeamIdlePct *float64 `json:"team_idle_pct"`
	Available   bool     `json:"available"`
}

// OverviewLive is a point-in-time dialer presence snapshot (not historical).
// Source: dialer session registry ListPresence (browser softphone + SIP branch).
type OverviewLive struct {
	Online int64 `json:"online"`  // connected to dialer
	InCall int64 `json:"in_call"` // busy (on a call or ringing)
	// Free is online and not busy (available to take a call).
	Free int64 `json:"free"`
	// IdleRatePct among online agents: free/online * 100 (ociosidade ao vivo).
	IdleRatePct *float64            `json:"idle_rate_pct"`
	BusyRatePct *float64            `json:"busy_rate_pct"`
	Agents      []OverviewLiveAgent `json:"agents,omitempty"`
	// HasData is true when the dialer registry was queried successfully.
	HasData bool      `json:"has_data"`
	AsOf    time.Time `json:"as_of"`
}

// OverviewLiveAgent is one online dialer contact.
type OverviewLiveAgent struct {
	UserID     string `json:"user_id"`
	Busy       bool   `json:"busy"`
	HasBrowser bool   `json:"has_browser"`
	HasBranch  bool   `json:"has_branch"`
}

// ChannelSlice is volume share by channel (entry_type) among engaged entries.
type ChannelSlice struct {
	Channel string  `json:"channel"` // whatsapp | voice
	Count   int64   `json:"count"`
	Pct     float64 `json:"pct"`
}

// OverviewMessaging is message intensity on conversations.
type OverviewMessaging struct {
	// Primary averages are over ENGAGED entries only (total_msgs > 0).
	AvgMessagesPerConversation *float64 `json:"avg_messages_per_conversation"`
	AvgInbound                 *float64 `json:"avg_inbound"`
	AvgOutbound                *float64 `json:"avg_outbound"`
	// Template messages (WhatsApp HSM / session openers). Counted separately so
	// ops can see billed-style template volume, not only free-form operator chat.
	AvgTemplate               *float64 `json:"avg_template,omitempty"`
	TemplateMessages          int64    `json:"template_messages"`
	ConversationsWithMessages int64    `json:"conversations_with_messages"`
	// Conversations that received at least one template message.
	ConversationsWithTemplate int64 `json:"conversations_with_template"`
	// Dual denominator: average including shells (zeros). Null when no scoped rows.
	AvgMessagesAllScoped *float64 `json:"avg_messages_all_scoped,omitempty"`
	Available            bool     `json:"available"`
}

// OverviewReopen is reopened conversations from conversation_events.
type OverviewReopen struct {
	ReopenedCount int64 `json:"reopened_count"`
	// FinishedCount is engaged finished (same universe as KPIs.Finished) for rate.
	FinishedCount int64 `json:"finished_count"`
	// FinishedEventCount is event_type=finished telemetry (may include out-of-scope shells).
	FinishedEventCount int64    `json:"finished_event_count"`
	ReopenRate         *float64 `json:"reopen_rate"` // reopened/finished_count * 100 when finished_count > 0
	Available          bool     `json:"available"`
}

// OverviewFinishedBySource splits engaged finished conversations by close_source.
// Human includes legacy empty source (pre-feature hand finishes, after backfill).
// Silence is system auto-close (customer_idle). Not telephony queue abandon.
type OverviewFinishedBySource struct {
	Human     int64    `json:"human"`  // intentional human (or legacy empty)
	AI        int64    `json:"ai"`     // AI finish tool
	System    int64    `json:"system"` // idle auto-close
	Total     int64    `json:"total"`  // human+ai+system (= finished when fully classified)
	HumanPct  *float64 `json:"human_pct,omitempty"`
	AIPct     *float64 `json:"ai_pct,omitempty"`
	SystemPct *float64 `json:"system_pct,omitempty"`
	Available bool     `json:"available"` // true when total > 0
}

// MetricDefinitions is returned so clients/docs stay aligned with the backend.
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
	Messaging        string `json:"messaging"`
	Reopen           string `json:"reopen"`
	FinishedBySource string `json:"finished_by_source"`
	Unassigned       string `json:"unassigned"`
	CSAT             string `json:"csat"`
	SLA              string `json:"sla"`
}

// Overview is the full attendance ops dashboard payload.
type Overview struct {
	Filter             OverviewFilter     `json:"filter"`
	KPIs               OverviewKPIs       `json:"kpis"`
	Hourly             []HourlyPoint      `json:"hourly"`
	StatusDistribution StatusDistribution `json:"status_distribution"`
	ByDepartment       []DepartmentRow    `json:"by_department"`
	ByMember           []MemberRow        `json:"by_member"`

	// Extended operational blocks
	FRT        OverviewFRT       `json:"frt"`
	AI         OverviewAI        `json:"ai"`
	Queue      OverviewQueue     `json:"queue"`
	Occupancy  OverviewOccupancy `json:"occupancy"`
	Live       OverviewLive      `json:"live"`
	ChannelMix []ChannelSlice    `json:"channel_mix"`
	Messaging  OverviewMessaging `json:"messaging"`
	Reopen     OverviewReopen    `json:"reopen"`
	// FinishedBySource splits KPIs.Finished by close_source for monthly review.
	FinishedBySource OverviewFinishedBySource `json:"finished_by_source"`

	Definitions MetricDefinitions `json:"definitions"`
}

// DefaultDefinitions documents the frozen contracts.
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
		Messaging:        "avg messages per engaged entry; avg_messages_all_scoped includes shells",
		Reopen:           "conversation_events reopened / engaged finished (KPI); finished_event_count is telemetry",
		FinishedBySource: "engaged finished by close_source: human (incl empty/legacy), ai, system",
		Unassigned:       "engaged entries with empty assignee and status != finished",
		CSAT:             "not available (csat_available=false) until surveys ship",
		SLA:              "not available (sla_available=false) until SLA policies ship",
	}
}

// GetOverviewUseCase is the single entry for the ops dashboard.
type GetOverviewUseCase interface {
	Execute(workspaceID string, filter OverviewFilter) (*Overview, error)
}
