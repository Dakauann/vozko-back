package advertising

import (
	"testing"
	"time"
)

func TestAnActiveObjectThatNeverDeliveredIsPreparingLikeMeta(t *testing.T) {
	now := time.Date(2026, 10, 4, 17, 0, 0, 0, time.UTC)
	fresh := &Object{EffectiveStatus: EffectiveActive}
	if got := fresh.Delivery(now); got != DeliveryPreparing {
		t.Fatalf("fresh active: %s", got)
	}
	delivered := now.Add(-time.Hour)
	running := &Object{EffectiveStatus: EffectiveActive, FirstDeliveredAt: &delivered}
	if got := running.Delivery(now); got != DeliveryActive {
		t.Fatalf("delivering: %s", got)
	}
	paused := &Object{EffectiveStatus: EffectivePaused}
	if got := paused.Delivery(now); got != DeliveryOff {
		t.Fatalf("paused: %s", got)
	}
}
