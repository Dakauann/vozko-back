package emailsending

import (
	"context"
	"math"
	"slices"
	"time"
)

const (
	SESSandboxMaxSendRate           = 1
	SESSandboxMax24HourSend         = 200
	PriorityDailyReserveBasisPoints = 500
	DailyQuotaAlertBasisPoints      = 8000
	SendQuotaStaleAfter             = 15 * time.Minute
	PriorityTokenWait               = 3 * time.Second
	DailyQuotaRetryAfter            = 15 * time.Minute
	basisPointsWhole                = 10000
)

type SendLane string

const (
	SendLanePriority SendLane = "priority"
	SendLaneCampaign SendLane = "campaign"
)

func SendLaneOf(kind RunKind) SendLane {
	if kind == RunCampaign {
		return SendLaneCampaign
	}
	return SendLanePriority
}

type SendQuota struct {
	MaxSendRate      float64
	Max24HourSend    float64
	SentLast24Hours  float64
	ProductionAccess bool
	Read             bool
	ReadAt           time.Time
}

func SandboxSendQuota(now time.Time) SendQuota {
	return SendQuota{MaxSendRate: SESSandboxMaxSendRate, Max24HourSend: SESSandboxMax24HourSend, ReadAt: now}
}

func (q SendQuota) TokensPerSecond() int64 {
	return max(1, int64(math.Floor(q.MaxSendRate)))
}

func (q SendQuota) Unlimited24Hours() bool {
	return q.Max24HourSend < 0
}

func (q SendQuota) DailyQuota() int64 {
	return int64(math.Floor(q.Max24HourSend))
}

func (q SendQuota) PriorityReserve() int64 {
	if q.Unlimited24Hours() {
		return 0
	}
	return ceilDiv(q.DailyQuota()*PriorityDailyReserveBasisPoints, basisPointsWhole)
}

func (q SendQuota) Used24Hours(sentSinceRead int64) int64 {
	return int64(math.Ceil(q.SentLast24Hours)) + sentSinceRead
}

func (q SendQuota) DailyRoom(lane SendLane, sentSinceRead int64) bool {
	if q.Unlimited24Hours() {
		return true
	}
	limit := q.DailyQuota()
	if lane == SendLaneCampaign {
		limit -= q.PriorityReserve()
	}
	return q.Used24Hours(sentSinceRead) < limit
}

func (q SendQuota) Fresh(now time.Time) bool {
	return !now.After(q.ReadAt.Add(SendQuotaStaleAfter))
}

func (q SendQuota) UsageBasisPoints(sentSinceRead int64) int64 {
	if q.Unlimited24Hours() {
		return 0
	}
	daily := q.DailyQuota()
	if daily <= 0 {
		return basisPointsWhole
	}
	return q.Used24Hours(sentSinceRead) * basisPointsWhole / daily
}

func (q SendQuota) NearlyExhausted(sentSinceRead int64) bool {
	return q.UsageBasisPoints(sentSinceRead) >= DailyQuotaAlertBasisPoints
}

func CampaignTokens(rate, priorityWanted int64) int64 {
	return max(0, rate-max(0, priorityWanted))
}

func FairShare(budget int64, workspaces []string, workspace string, second int64) int64 {
	if budget <= 0 {
		return 0
	}
	members := slices.Clone(workspaces)
	if !slices.Contains(members, workspace) {
		members = append(members, workspace)
	}
	slices.Sort(members)
	members = slices.Compact(members)
	n := int64(len(members))
	position := int64(slices.Index(members, workspace))
	rotated := ((position-second)%n + n) % n
	share := budget / n
	if rotated < budget%n {
		share++
	}
	return share
}

func SendWindow(at time.Time) int64 {
	return at.Unix()
}

func UntilNextSendWindow(at time.Time) time.Duration {
	return time.Unix(at.Unix()+1, 0).Sub(at)
}

func ceilDiv(a, b int64) int64 {
	return (a + b - 1) / b
}

type SendTicket struct {
	WorkspaceID string
	RunKind     RunKind
}

type SendPacer interface {
	Admit(ctx context.Context, ticket SendTicket) error
}

type SendQuotaReader interface {
	ReadSendQuota(ctx context.Context) (SendQuota, error)
}
