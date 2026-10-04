package advertising

import "time"

type BillingKind string

const (
	BillingPrepaid  BillingKind = "prepaid"
	BillingPostpaid BillingKind = "postpaid"
	BillingUnknown  BillingKind = "unknown"
)

func BillingKindOf(prepay bool) BillingKind {
	if prepay {
		return BillingPrepaid
	}
	return BillingPostpaid
}

type FundsLevel string

const (
	FundsOK            FundsLevel = "ok"
	FundsLow           FundsLevel = "low"
	FundsOut           FundsLevel = "out"
	FundsPaymentFailed FundsLevel = "payment_failed"
	FundsUnknown       FundsLevel = "unknown"
)

func (l FundsLevel) NeedsAttention() bool {
	return l == FundsLow || l == FundsOut || l == FundsPaymentFailed
}

var AttentionFundsLevels = []FundsLevel{FundsLow, FundsOut, FundsPaymentFailed}

type FundsReason string

const (
	ReasonFundsLow          FundsReason = "funds_low"
	ReasonFundsOut          FundsReason = "funds_out"
	ReasonSpendLimitLow     FundsReason = "spend_limit_low"
	ReasonSpendLimitReached FundsReason = "spend_limit_reached"
	ReasonPaymentFailed     FundsReason = "payment_failed"
	ReasonGracePeriod       FundsReason = "grace_period"
	ReasonBillingUnreadable FundsReason = "billing_unreadable"
)

const (
	DisableReasonRiskPayment = 3
	FundsPaceDays            = 7
	fundsLowDays             = 3
	fundsLowShareDivisor     = 5
)

type Funds struct {
	Kind             BillingKind
	Level            FundsLevel
	Reason           FundsReason
	LimitMinor       *int64
	SpentMinor       int64
	RoomMinor        *int64
	DailySpendMicros int64
	DaysLeft         *float64
}

func FundsOf(a *AdAccount) Funds {
	f := Funds{Kind: a.Billing, SpentMinor: a.AmountSpent, DailySpendMicros: a.DailySpendMicros, LimitMinor: a.SpendCapLimit()}
	if f.Kind == "" {
		f.Kind = BillingUnknown
	}
	switch {
	case a.paymentFailed():
		return f.at(FundsPaymentFailed, ReasonPaymentFailed)
	case a.MetaStatus == MetaAccountInGracePeriod:
		return f.at(FundsPaymentFailed, ReasonGracePeriod)
	case a.CanRead() != nil || f.Kind == BillingUnknown:
		return f.at(FundsUnknown, ReasonBillingUnreadable)
	}
	if f.LimitMinor == nil {
		if f.Kind == BillingPrepaid {
			return f.at(FundsUnknown, ReasonBillingUnreadable)
		}
		return f.at(FundsOK, "")
	}
	room := max(*f.LimitMinor-f.SpentMinor, 0)
	f.RoomMinor = &room
	f.DaysLeft = daysLeft(a.Currency, room, f.DailySpendMicros)
	prepaid := f.Kind == BillingPrepaid
	switch {
	case room == 0 && prepaid:
		return f.at(FundsOut, ReasonFundsOut)
	case room == 0:
		return f.at(FundsOut, ReasonSpendLimitReached)
	case !f.lowRoom(room):
		return f.at(FundsOK, "")
	case prepaid:
		return f.at(FundsLow, ReasonFundsLow)
	}
	return f.at(FundsLow, ReasonSpendLimitLow)
}

func (f Funds) at(level FundsLevel, reason FundsReason) Funds {
	f.Level, f.Reason = level, reason
	return f
}

func (f Funds) lowRoom(room int64) bool {
	if room*fundsLowShareDivisor < *f.LimitMinor {
		return true
	}
	return f.DaysLeft != nil && *f.DaysLeft < fundsLowDays
}

func daysLeft(currency string, roomMinor, dailySpendMicros int64) *float64 {
	if dailySpendMicros <= 0 {
		return nil
	}
	roomMicros, err := MinorToMicros(currency, roomMinor)
	if err != nil {
		return nil
	}
	days := float64(roomMicros) / float64(dailySpendMicros)
	return &days
}

func (a *AdAccount) paymentFailed() bool {
	switch a.MetaStatus {
	case MetaAccountUnsettled, MetaAccountPendingSettlement:
		return true
	}
	return a.DisableReason == DisableReasonRiskPayment
}

func (a *AdAccount) RecordFunds(level FundsLevel, now time.Time) bool {
	if a.FundsLevel == level && a.FundsLevelSince != nil {
		return false
	}
	a.FundsLevel = level
	a.FundsLevelSince = &now
	return true
}

func AverageDailySpend(rows []DailyInsight, days int) int64 {
	if days <= 0 {
		return 0
	}
	var total int64
	for _, row := range rows {
		total += row.SpendMicros
	}
	return total / int64(days)
}

func (a *AdAccount) CanSetSpendCap() error {
	switch a.Billing {
	case BillingPrepaid:
		return ErrPrepaidSpendCap
	case BillingPostpaid:
		return nil
	}
	return ErrBillingUnknown
}
