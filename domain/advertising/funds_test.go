package advertising

import (
	"testing"
	"time"
)

func fundedAccount(kind BillingKind, spendCap, spent, dailySpendMicros int64) *AdAccount {
	synced := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	return &AdAccount{
		MetaStatus: MetaAccountActive, Connection: ConnectionConnected, HasFunding: true, Currency: "BRL",
		Billing: kind, SpendCap: spendCap, AmountSpent: spent, DailySpendMicros: dailySpendMicros, LastSyncedAt: &synced,
	}
}

func TestFundsOfAPrepaidAccountComesFromTheLimitMetaKeepsEqualToTheFunds(t *testing.T) {
	f := FundsOf(fundedAccount(BillingPrepaid, 2635, 125, 130_000))
	if f.Level != FundsOK || f.Kind != BillingPrepaid {
		t.Fatalf("funds %+v", f)
	}
	if f.RoomMinor == nil || *f.RoomMinor != 2510 || f.LimitMinor == nil || *f.LimitMinor != 2635 {
		t.Fatalf("room %v limit %v", f.RoomMinor, f.LimitMinor)
	}
	if f.DaysLeft == nil || *f.DaysLeft < 193 || *f.DaysLeft > 194 {
		t.Fatalf("days left %v", f.DaysLeft)
	}
}

func TestFundsLevels(t *testing.T) {
	cases := map[string]struct {
		account *AdAccount
		level   FundsLevel
		reason  FundsReason
	}{
		"prepaid at zero":                {fundedAccount(BillingPrepaid, 2635, 2635, 100_000), FundsOut, ReasonFundsOut},
		"spent past the limit":           {fundedAccount(BillingPrepaid, 2635, 2700, 100_000), FundsOut, ReasonFundsOut},
		"postpaid limit reached":         {fundedAccount(BillingPostpaid, 10000, 10000, 0), FundsOut, ReasonSpendLimitReached},
		"under a fifth of the limit":     {fundedAccount(BillingPrepaid, 10000, 8100, 0), FundsLow, ReasonFundsLow},
		"under three days at this pace":  {fundedAccount(BillingPrepaid, 10000, 5000, 20_000_000), FundsLow, ReasonFundsLow},
		"postpaid limit almost reached":  {fundedAccount(BillingPostpaid, 10000, 9000, 0), FundsLow, ReasonSpendLimitLow},
		"three days left exactly is ok":  {fundedAccount(BillingPrepaid, 10000, 4000, 20_000_000), FundsOK, ""},
		"postpaid without a limit":       {fundedAccount(BillingPostpaid, 0, 99999, 50_000_000), FundsOK, ""},
		"prepaid without a limit":        {fundedAccount(BillingPrepaid, 0, 125, 0), FundsUnknown, ReasonBillingUnreadable},
		"billing never read":             {fundedAccount(BillingUnknown, 2635, 125, 0), FundsUnknown, ReasonBillingUnreadable},
		"no spend yet is not days-low":   {fundedAccount(BillingPrepaid, 10000, 0, 0), FundsOK, ""},
	}
	for name, tc := range cases {
		f := FundsOf(tc.account)
		if f.Level != tc.level || f.Reason != tc.reason {
			t.Errorf("%s: got %s/%s, want %s/%s", name, f.Level, f.Reason, tc.level, tc.reason)
		}
		if f.RoomMinor != nil && *f.RoomMinor < 0 {
			t.Errorf("%s: room went negative: %d", name, *f.RoomMinor)
		}
	}
}

func TestAPaymentProblemWinsOverEverythingElse(t *testing.T) {
	cases := map[string]struct {
		status MetaAccountStatus
		reason int
		want   FundsReason
	}{
		"unsettled":          {MetaAccountUnsettled, 0, ReasonPaymentFailed},
		"pending settlement": {MetaAccountPendingSettlement, 0, ReasonPaymentFailed},
		"grace period":       {MetaAccountInGracePeriod, 0, ReasonGracePeriod},
		"disabled for risk":  {MetaAccountDisabled, DisableReasonRiskPayment, ReasonPaymentFailed},
	}
	for name, tc := range cases {
		account := fundedAccount(BillingUnknown, 2635, 125, 0)
		account.MetaStatus, account.DisableReason = tc.status, tc.reason
		f := FundsOf(account)
		if f.Level != FundsPaymentFailed || f.Reason != tc.want {
			t.Errorf("%s: got %s/%s", name, f.Level, f.Reason)
		}
	}
}

func TestFundsAreUnknownWhenTheAccountCannotBeRead(t *testing.T) {
	account := fundedAccount(BillingPrepaid, 2635, 125, 0)
	account.Connection = ConnectionNeedsReconnect
	if f := FundsOf(account); f.Level != FundsUnknown {
		t.Fatalf("a disconnected account must not look funded: %+v", f)
	}
}

func TestOnlyLowOutAndFailedPaymentsNeedAttention(t *testing.T) {
	for level, want := range map[FundsLevel]bool{FundsOK: false, FundsUnknown: false, FundsLow: true, FundsOut: true, FundsPaymentFailed: true} {
		if got := level.NeedsAttention(); got != want {
			t.Errorf("%s: NeedsAttention = %v", level, got)
		}
	}
}

func TestRecordingFundsKeepsWhenTheLevelStarted(t *testing.T) {
	account := fundedAccount(BillingPrepaid, 2635, 125, 0)
	first := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	if !account.RecordFunds(FundsLow, first) || account.FundsLevel != FundsLow || !account.FundsLevelSince.Equal(first) {
		t.Fatalf("first record %+v", account)
	}
	if account.RecordFunds(FundsLow, first.Add(time.Hour)) || !account.FundsLevelSince.Equal(first) {
		t.Fatal("the same level must keep its start time")
	}
	if !account.RecordFunds(FundsOut, first.Add(2*time.Hour)) || !account.FundsLevelSince.Equal(first.Add(2*time.Hour)) {
		t.Fatal("a new level must start a new period")
	}
}

func TestAverageDailySpendCountsEveryDayOfTheWindow(t *testing.T) {
	day := func(d int, micros int64) DailyInsight {
		return DailyInsight{Day: time.Date(2026, 10, d, 0, 0, 0, 0, time.UTC), SpendMicros: micros}
	}
	rows := []DailyInsight{day(1, 700_000), day(1, 700_000), day(3, 0)}
	if got := AverageDailySpend(rows, 7); got != 200_000 {
		t.Fatalf("average %d", got)
	}
	if AverageDailySpend(nil, 7) != 0 || AverageDailySpend(rows, 0) != 0 {
		t.Fatal("no rows or no window means no pace")
	}
}

func TestPrepaidAccountsCannotTakeASpendCap(t *testing.T) {
	if err := fundedAccount(BillingPrepaid, 2635, 125, 0).CanSetSpendCap(); err != ErrPrepaidSpendCap {
		t.Fatalf("prepaid: %v", err)
	}
	if err := fundedAccount(BillingPostpaid, 0, 125, 0).CanSetSpendCap(); err != nil {
		t.Fatalf("postpaid: %v", err)
	}
	if err := fundedAccount(BillingUnknown, 0, 125, 0).CanSetSpendCap(); err != ErrBillingUnknown {
		t.Fatalf("unknown: %v", err)
	}
}
