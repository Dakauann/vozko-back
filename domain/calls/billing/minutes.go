package billing

import (
	"math"
	"time"
)

const BillingMinute = time.Minute

func BillableSeconds(answeredAt, endedAt time.Time) int {
	if answeredAt.IsZero() {
		return 0
	}
	talk := endedAt.Sub(answeredAt)
	if talk <= 0 {
		return 1
	}
	return int(math.Ceil(talk.Seconds()))
}

func BilledMinutes(billableSeconds int) int64 {
	if billableSeconds <= 0 {
		return 0
	}
	return int64((billableSeconds + 59) / 60)
}

type MinuteWaves struct {
	AnsweredAt time.Time
	Minute     time.Duration
	Lead       time.Duration
}

func (w MinuteWaves) CoverageEnd(reservedMinutes int64) time.Time {
	return w.AnsweredAt.Add(time.Duration(reservedMinutes) * w.Minute)
}

func (w MinuteWaves) NextReservationAt(reservedMinutes int64) time.Time {
	at := w.CoverageEnd(reservedMinutes).Add(-w.Lead)
	if at.Before(w.AnsweredAt) {
		return w.AnsweredAt
	}
	return at
}
