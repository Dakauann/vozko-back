package calllist

import "time"

type AgendaSegment int

const (
	SegmentDue AgendaSegment = iota
	SegmentQueue
	SegmentWaiting
)

func AgendaSegmentOf(callbackAt *time.Time, asOf time.Time) AgendaSegment {
	switch {
	case callbackAt == nil:
		return SegmentQueue
	case callbackAt.After(asOf):
		return SegmentWaiting
	default:
		return SegmentDue
	}
}

func (q ItemQuery) Agenda() bool {
	return q.State == StatePending
}

func (q ItemQuery) Continues() bool {
	return q.AfterPosition > 0 || q.AfterAt != nil
}

func (q ItemQuery) CursorSegment() AgendaSegment {
	if !q.Continues() {
		return SegmentDue
	}
	return AgendaSegmentOf(q.AfterAt, q.AsOf)
}

func (q ItemQuery) Check() error {
	if q.Agenda() && q.AsOf.IsZero() {
		return ErrItemCursorInvalid
	}
	return nil
}
