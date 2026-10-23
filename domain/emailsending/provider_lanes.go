package emailsending

import (
	"context"
	"time"
)

const SESManagementCallInterval = time.Second

type ProviderLane int

const (
	LaneProtect ProviderLane = iota
	LaneAccount
	LaneBackground
)

var ProviderLanes = []ProviderLane{LaneProtect, LaneAccount, LaneBackground}

var sesSendingActions = map[string]bool{
	"SendEmail":          true,
	"SendRawEmail":       true,
	"SendTemplatedEmail": true,
}

func SESManagementCall(operation string) bool {
	return !sesSendingActions[operation]
}

func (l ProviderLane) Valid() bool {
	return l >= LaneProtect && l <= LaneBackground
}

func (l ProviderLane) Outranks(other ProviderLane) bool {
	return l < other
}

func (l ProviderLane) String() string {
	switch l {
	case LaneProtect:
		return "protect"
	case LaneAccount:
		return "account"
	case LaneBackground:
		return "background"
	}
	return "unknown"
}

type providerLaneKey struct{}

func WithProviderLane(ctx context.Context, lane ProviderLane) context.Context {
	return context.WithValue(ctx, providerLaneKey{}, lane)
}

func ProviderLaneOf(ctx context.Context) ProviderLane {
	if lane, ok := ctx.Value(providerLaneKey{}).(ProviderLane); ok && lane.Valid() {
		return lane
	}
	return LaneAccount
}

type ProviderCallLimiter interface {
	Await(ctx context.Context, lane ProviderLane) error
}

func LaneMayCall(lane ProviderLane, waiting func(ProviderLane) bool) bool {
	for _, other := range ProviderLanes {
		if other.Outranks(lane) && waiting(other) {
			return false
		}
	}
	return true
}
