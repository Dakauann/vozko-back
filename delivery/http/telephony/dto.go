package telephony

import (
	"time"

	telephonydomain "vozko/domain/telephony"
)

type OverviewFilterResponse struct {
	DateFrom            *time.Time `json:"date_from,omitempty" example:"2026-07-01T00:00:00Z"`
	DateTo              *time.Time `json:"date_to,omitempty" example:"2026-07-19T23:59:59Z"`
	Direction           string     `json:"direction,omitempty" example:"inbound"`
	CallType            string     `json:"call_type,omitempty" example:"crm"`
	AgentID             string     `json:"agent_id,omitempty" example:"agt_a1b2c3"`
	MemberID            string     `json:"member_id,omitempty" example:"usr_a1b2c3"`
	ServiceLevelSeconds int        `json:"service_level_seconds,omitempty" example:"20"`
}

type OverviewKPIsResponse struct {
	TotalCalls          int64    `json:"total_calls" example:"1240"`
	Answered            int64    `json:"answered" example:"1080"`
	Failed              int64    `json:"failed" example:"90"`
	Abandoned           int64    `json:"abandoned" example:"70"`
	ConnectRate         *float64 `json:"connect_rate" example:"87.1"`
	Inbound             int64    `json:"inbound" example:"720"`
	Outbound            int64    `json:"outbound" example:"520"`
	AvgRingMins         *float64 `json:"avg_ring_mins" example:"0.3"`
	AvgTalkMins         *float64 `json:"avg_talk_mins" example:"3.4"`
	AvgHandleMins       *float64 `json:"avg_handle_mins" example:"3.7"`
	AvgAHTMins          *float64 `json:"avg_aht_mins" example:"4.1"`
	AvgHoldMins         *float64 `json:"avg_hold_mins" example:"0.5"`
	AvgACWMins          *float64 `json:"avg_acw_mins" example:"0.2"`
	HumanCRMCalls       int64    `json:"human_crm_calls" example:"510"`
	TrunkInbound        int64    `json:"trunk_inbound" example:"180"`
	TrunkOutbound       int64    `json:"trunk_outbound" example:"120"`
	ServiceLevelPct     *float64 `json:"service_level_pct" example:"82.5"`
	ServiceLevelSeconds int      `json:"service_level_seconds" example:"20"`
	AnsweredWithinSL    int64    `json:"answered_within_sl" example:"890"`
	CDRAbandonRate      *float64 `json:"cdr_abandon_rate" example:"5.6"`
	ShortAbandons       int64    `json:"short_abandons" example:"24"`
	Transfers           int64    `json:"transfers" example:"58"`
}

type HourlyPointResponse struct {
	Hour  int   `json:"hour" example:"14"`
	Count int64 `json:"count" example:"87"`
}

type TypeSliceResponse struct {
	Type  string  `json:"type" example:"crm"`
	Count int64   `json:"count" example:"430"`
	Pct   float64 `json:"pct" example:"34.7"`
}

type DirectionSliceResponse struct {
	Direction string  `json:"direction" example:"inbound"`
	Count     int64   `json:"count" example:"720"`
	Pct       float64 `json:"pct" example:"58.1"`
}

type DispositionSliceResponse struct {
	Code  string  `json:"code" example:"answered"`
	Label string  `json:"label,omitempty" example:"Atendida"`
	Count int64   `json:"count" example:"1080"`
	Pct   float64 `json:"pct" example:"87.1"`
}

type QueueBlockResponse struct {
	Enqueued            int64    `json:"enqueued" example:"640"`
	Connected           int64    `json:"connected" example:"590"`
	Abandoned           int64    `json:"abandoned" example:"50"`
	Overflow            int64    `json:"overflow" example:"0"`
	QueueFull           int64    `json:"queue_full" example:"0"`
	Cancelled           int64    `json:"cancelled" example:"12"`
	AvgASAMins          *float64 `json:"avg_asa_mins" example:"0.4"`
	MaxWaitMins         *float64 `json:"max_wait_mins" example:"2.1"`
	ServiceLevelPct     *float64 `json:"service_level_pct" example:"84.0"`
	ServiceLevelSeconds int      `json:"service_level_seconds" example:"20"`
	AbandonRate         float64  `json:"abandon_rate" example:"7.8"`
	Available           bool     `json:"available" example:"true"`
}

type OccupancyBlockResponse struct {
	AvgOccupancyPct  *float64 `json:"avg_occupancy_pct" example:"72.5"`
	TeamOccupancyPct *float64 `json:"team_occupancy_pct" example:"70.1"`
	TeamIdlePct      *float64 `json:"team_idle_pct" example:"29.9"`
	AgentsSampled    int64    `json:"agents_sampled" example:"12"`
	Available        bool     `json:"available" example:"true"`
}

type LiveAgentResponse struct {
	UserID     string `json:"user_id" example:"usr_a1b2c3"`
	Busy       bool   `json:"busy" example:"true"`
	HasBrowser bool   `json:"has_browser" example:"true"`
	HasBranch  bool   `json:"has_branch" example:"false"`
}

type LiveBlockResponse struct {
	Online      int64               `json:"online" example:"8"`
	InCall      int64               `json:"in_call" example:"5"`
	Free        int64               `json:"free" example:"3"`
	IdleRatePct *float64            `json:"idle_rate_pct" example:"37.5"`
	BusyRatePct *float64            `json:"busy_rate_pct" example:"62.5"`
	Agents      []LiveAgentResponse `json:"agents,omitempty"`
	HasData     bool                `json:"has_data" example:"true"`
	AsOf        time.Time           `json:"as_of" example:"2026-07-19T14:35:00Z"`
}

type MemberRowResponse struct {
	UserID        string   `json:"user_id" example:"usr_a1b2c3"`
	TotalCalls    int64    `json:"total_calls" example:"120"`
	Answered      int64    `json:"answered" example:"108"`
	Failed        int64    `json:"failed" example:"8"`
	Abandoned     int64    `json:"abandoned" example:"4"`
	ConnectRate   float64  `json:"connect_rate" example:"90.0"`
	AvgTalkMins   *float64 `json:"avg_talk_mins" example:"3.5"`
	AvgRingMins   *float64 `json:"avg_ring_mins" example:"0.3"`
	AvgHandleMins *float64 `json:"avg_handle_mins" example:"3.8"`
	WithinSL      int64    `json:"within_sl" example:"96"`
	ServiceLevel  *float64 `json:"service_level_pct,omitempty" example:"88.9"`
	OccupancyPct  *float64 `json:"occupancy_pct,omitempty" example:"74.2"`
	IdlePct       *float64 `json:"idle_pct,omitempty" example:"25.8"`
}

type MetricDefinitionsResponse struct {
	ConnectRate  string `json:"connect_rate" example:"chamadas atendidas / tentativas no período * 100"`
	RingTime     string `json:"ring_time" example:"answered_at - started_at (min) para chamadas atendidas"`
	TalkTime     string `json:"talk_time" example:"ended_at - answered_at (min) quando ambos definidos"`
	AHT          string `json:"aht" example:"conversa + espera + pós-atendimento (aproximação de TMA)"`
	ASA          string `json:"asa" example:"média de queue_events.waited_ms para conectados (min)"`
	ServiceLevel string `json:"service_level" example:"atendidas com toque <= 20s / atendidas * 100"`
	Occupancy    string `json:"occupancy" example:"on_call_ms / online_ms dos intervalos de presença"`
	Idle         string `json:"idle" example:"ao vivo: livres/online; histórico: 100 - ocupação"`
	Disposition  string `json:"disposition" example:"pilhas de calls.status + end_reason / surrender_reason"`
	ByMember     string `json:"by_member" example:"CDR humano por agente (usuário do discador) agrupado"`
	Persistence  string `json:"persistence" example:"métricas de tabelas duráveis exceto o registro ao vivo"`
	Gaps         string `json:"gaps" example:"captura real de espera/ACW no caminho quente, ver docs"`
}

type OverviewResponse struct {
	Filter       OverviewFilterResponse     `json:"filter"`
	KPIs         OverviewKPIsResponse       `json:"kpis"`
	Hourly       []HourlyPointResponse      `json:"hourly"`
	ByType       []TypeSliceResponse        `json:"by_type"`
	ByDirection  []DirectionSliceResponse   `json:"by_direction"`
	Dispositions []DispositionSliceResponse `json:"dispositions"`
	Queue        QueueBlockResponse         `json:"queue"`
	Occupancy    OccupancyBlockResponse     `json:"occupancy"`
	Live         LiveBlockResponse          `json:"live"`
	ByMember     []MemberRowResponse        `json:"by_member"`
	SLAAvailable bool                       `json:"sla_available" example:"true"`
	Definitions  MetricDefinitionsResponse  `json:"definitions"`
}

type CapacityResponse struct {
	Used int64   `json:"used" example:"5"`
	Max  int64   `json:"max" example:"10"`
	Pct  float64 `json:"pct" example:"50.0"`
}

type HumanSeatResponse struct {
	UserID     string    `json:"user_id" example:"usr_a1b2c3"`
	Username   string    `json:"username,omitempty" example:"Ana Souza"`
	State      string    `json:"state" example:"on_call"`
	HasBrowser bool      `json:"has_browser" example:"true"`
	HasBranch  bool      `json:"has_branch" example:"false"`
	Since      time.Time `json:"since,omitempty" example:"2026-07-19T14:30:00Z"`
}

type QueueStripResponse struct {
	Depth     int64 `json:"depth" example:"3"`
	Available bool  `json:"available" example:"true"`
}

type BoardSnapshotResponse struct {
	WorkspaceID string              `json:"workspace_id" example:"ws_a1b2c3"`
	Rev         int64               `json:"rev" example:"42"`
	AsOf        time.Time           `json:"as_of" example:"2026-07-19T14:35:00Z"`
	Capacity    CapacityResponse    `json:"capacity"`
	Humans      []HumanSeatResponse `json:"humans"`
	Queue       QueueStripResponse  `json:"queue"`
	Online      int64               `json:"online" example:"8"`
	Free        int64               `json:"free" example:"3"`
	InCall      int64               `json:"in_call" example:"5"`
	Ringing     int64               `json:"ringing" example:"0"`
	IdlePct     *float64            `json:"idle_pct,omitempty" example:"37.5"`
}

func toOverviewResponse(o *telephonydomain.Overview) OverviewResponse {
	return OverviewResponse{
		Filter:       toOverviewFilterResponse(o.Filter),
		KPIs:         toOverviewKPIsResponse(o.KPIs),
		Hourly:       toHourlyPointsResponse(o.Hourly),
		ByType:       toTypeSlicesResponse(o.ByType),
		ByDirection:  toDirectionSlicesResponse(o.ByDirection),
		Dispositions: toDispositionSlicesResponse(o.Dispositions),
		Queue:        toQueueBlockResponse(o.Queue),
		Occupancy:    toOccupancyBlockResponse(o.Occupancy),
		Live:         toLiveBlockResponse(o.Live),
		ByMember:     toMemberRowsResponse(o.ByMember),
		SLAAvailable: o.SLAAvailable,
		Definitions:  toMetricDefinitionsResponse(o.Definitions),
	}
}

func toOverviewFilterResponse(f telephonydomain.OverviewFilter) OverviewFilterResponse {
	return OverviewFilterResponse{
		DateFrom:            f.DateFrom,
		DateTo:              f.DateTo,
		Direction:           f.Direction,
		CallType:            f.CallType,
		AgentID:             f.AgentID,
		MemberID:            f.MemberID,
		ServiceLevelSeconds: f.ServiceLevelSeconds,
	}
}

func toOverviewKPIsResponse(k telephonydomain.OverviewKPIs) OverviewKPIsResponse {
	return OverviewKPIsResponse{
		TotalCalls:          k.TotalCalls,
		Answered:            k.Answered,
		Failed:              k.Failed,
		Abandoned:           k.Abandoned,
		ConnectRate:         k.ConnectRate,
		Inbound:             k.Inbound,
		Outbound:            k.Outbound,
		AvgRingMins:         k.AvgRingMins,
		AvgTalkMins:         k.AvgTalkMins,
		AvgHandleMins:       k.AvgHandleMins,
		AvgAHTMins:          k.AvgAHTMins,
		AvgHoldMins:         k.AvgHoldMins,
		AvgACWMins:          k.AvgACWMins,
		HumanCRMCalls:       k.HumanCRMCalls,
		TrunkInbound:        k.TrunkInbound,
		TrunkOutbound:       k.TrunkOutbound,
		ServiceLevelPct:     k.ServiceLevelPct,
		ServiceLevelSeconds: k.ServiceLevelSeconds,
		AnsweredWithinSL:    k.AnsweredWithinSL,
		CDRAbandonRate:      k.CDRAbandonRate,
		ShortAbandons:       k.ShortAbandons,
		Transfers:           k.Transfers,
	}
}

func toHourlyPointsResponse(src []telephonydomain.HourlyPoint) []HourlyPointResponse {
	if src == nil {
		return nil
	}
	out := make([]HourlyPointResponse, len(src))
	for i, p := range src {
		out[i] = HourlyPointResponse{Hour: p.Hour, Count: p.Count}
	}
	return out
}

func toTypeSlicesResponse(src []telephonydomain.TypeSlice) []TypeSliceResponse {
	if src == nil {
		return nil
	}
	out := make([]TypeSliceResponse, len(src))
	for i, s := range src {
		out[i] = TypeSliceResponse{Type: s.Type, Count: s.Count, Pct: s.Pct}
	}
	return out
}

func toDirectionSlicesResponse(src []telephonydomain.DirectionSlice) []DirectionSliceResponse {
	if src == nil {
		return nil
	}
	out := make([]DirectionSliceResponse, len(src))
	for i, s := range src {
		out[i] = DirectionSliceResponse{Direction: s.Direction, Count: s.Count, Pct: s.Pct}
	}
	return out
}

func toDispositionSlicesResponse(src []telephonydomain.DispositionSlice) []DispositionSliceResponse {
	if src == nil {
		return nil
	}
	out := make([]DispositionSliceResponse, len(src))
	for i, s := range src {
		out[i] = DispositionSliceResponse{Code: s.Code, Label: s.Label, Count: s.Count, Pct: s.Pct}
	}
	return out
}

func toQueueBlockResponse(q telephonydomain.QueueBlock) QueueBlockResponse {
	return QueueBlockResponse{
		Enqueued:            q.Enqueued,
		Connected:           q.Connected,
		Abandoned:           q.Abandoned,
		Overflow:            q.Overflow,
		QueueFull:           q.QueueFull,
		Cancelled:           q.Cancelled,
		AvgASAMins:          q.AvgASAMins,
		MaxWaitMins:         q.MaxWaitMins,
		ServiceLevelPct:     q.ServiceLevelPct,
		ServiceLevelSeconds: q.ServiceLevelSeconds,
		AbandonRate:         q.AbandonRate,
		Available:           q.Available,
	}
}

func toOccupancyBlockResponse(o telephonydomain.OccupancyBlock) OccupancyBlockResponse {
	return OccupancyBlockResponse{
		AvgOccupancyPct:  o.AvgOccupancyPct,
		TeamOccupancyPct: o.TeamOccupancyPct,
		TeamIdlePct:      o.TeamIdlePct,
		AgentsSampled:    o.AgentsSampled,
		Available:        o.Available,
	}
}

func toLiveBlockResponse(l telephonydomain.LiveBlock) LiveBlockResponse {
	return LiveBlockResponse{
		Online:      l.Online,
		InCall:      l.InCall,
		Free:        l.Free,
		IdleRatePct: l.IdleRatePct,
		BusyRatePct: l.BusyRatePct,
		Agents:      toLiveAgentsResponse(l.Agents),
		HasData:     l.HasData,
		AsOf:        l.AsOf,
	}
}

func toLiveAgentsResponse(src []telephonydomain.LiveAgent) []LiveAgentResponse {
	if src == nil {
		return nil
	}
	out := make([]LiveAgentResponse, len(src))
	for i, a := range src {
		out[i] = LiveAgentResponse{UserID: a.UserID, Busy: a.Busy, HasBrowser: a.HasBrowser, HasBranch: a.HasBranch}
	}
	return out
}

func toMemberRowsResponse(src []telephonydomain.MemberRow) []MemberRowResponse {
	if src == nil {
		return nil
	}
	out := make([]MemberRowResponse, len(src))
	for i, m := range src {
		out[i] = MemberRowResponse{
			UserID:        m.UserID,
			TotalCalls:    m.TotalCalls,
			Answered:      m.Answered,
			Failed:        m.Failed,
			Abandoned:     m.Abandoned,
			ConnectRate:   m.ConnectRate,
			AvgTalkMins:   m.AvgTalkMins,
			AvgRingMins:   m.AvgRingMins,
			AvgHandleMins: m.AvgHandleMins,
			WithinSL:      m.WithinSL,
			ServiceLevel:  m.ServiceLevel,
			OccupancyPct:  m.OccupancyPct,
			IdlePct:       m.IdlePct,
		}
	}
	return out
}

func toMetricDefinitionsResponse(d telephonydomain.MetricDefinitions) MetricDefinitionsResponse {
	return MetricDefinitionsResponse{
		ConnectRate:  d.ConnectRate,
		RingTime:     d.RingTime,
		TalkTime:     d.TalkTime,
		AHT:          d.AHT,
		ASA:          d.ASA,
		ServiceLevel: d.ServiceLevel,
		Occupancy:    d.Occupancy,
		Idle:         d.Idle,
		Disposition:  d.Disposition,
		ByMember:     d.ByMember,
		Persistence:  d.Persistence,
		Gaps:         d.Gaps,
	}
}

func toBoardSnapshotResponse(b *telephonydomain.BoardSnapshot) BoardSnapshotResponse {
	return BoardSnapshotResponse{
		WorkspaceID: b.WorkspaceID,
		Rev:         b.Rev,
		AsOf:        b.AsOf,
		Capacity: CapacityResponse{
			Used: b.Capacity.Used,
			Max:  b.Capacity.Max,
			Pct:  b.Capacity.Pct,
		},
		Humans: toHumanSeatsResponse(b.Humans),
		Queue: QueueStripResponse{
			Depth:     b.Queue.Depth,
			Available: b.Queue.Available,
		},
		Online:  b.Online,
		Free:    b.Free,
		InCall:  b.InCall,
		Ringing: b.Ringing,
		IdlePct: b.IdlePct,
	}
}

func toHumanSeatsResponse(src []telephonydomain.HumanSeat) []HumanSeatResponse {
	if src == nil {
		return nil
	}
	out := make([]HumanSeatResponse, len(src))
	for i, s := range src {
		out[i] = HumanSeatResponse{
			UserID:     s.UserID,
			Username:   s.Username,
			State:      string(s.State),
			HasBrowser: s.HasBrowser,
			HasBranch:  s.HasBranch,
			Since:      s.Since,
		}
	}
	return out
}
