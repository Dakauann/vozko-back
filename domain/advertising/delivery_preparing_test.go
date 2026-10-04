package advertising

import (
	"testing"
	"time"
)

func TestAnApprovedObjectReadsActiveLikeMetaAndTellsWhetherItEverDelivered(t *testing.T) {
	now := time.Date(2026, 10, 4, 17, 0, 0, 0, time.UTC)
	fresh := &Object{EffectiveStatus: EffectiveActive}
	if fresh.Delivery(now) != DeliveryActive || fresh.Delivered() {
		t.Fatalf("fresh: %s delivered %v", fresh.Delivery(now), fresh.Delivered())
	}
	delivered := now.Add(-time.Hour)
	running := &Object{EffectiveStatus: EffectiveActive, FirstDeliveredAt: &delivered}
	if running.Delivery(now) != DeliveryActive || !running.Delivered() {
		t.Fatalf("running: %s delivered %v", running.Delivery(now), running.Delivered())
	}
}
