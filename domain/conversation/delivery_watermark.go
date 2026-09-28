package conversation

import (
	"time"

	"vozko/domain/shared"
)

type DeliveryWatermarkRepository interface {
	MarkOutboundStatusUpTo(entryID string, entryType shared.EntryType, status DeliveryStatus, upTo time.Time) (int64, error)
}

func (s DeliveryStatus) Supersedes() []DeliveryStatus {
	switch s {
	case DeliveryStatusDelivered:
		return []DeliveryStatus{DeliveryStatusNone, DeliveryStatusSent}
	case DeliveryStatusRead:
		return []DeliveryStatus{DeliveryStatusNone, DeliveryStatusSent, DeliveryStatusDelivered}
	}
	return nil
}
