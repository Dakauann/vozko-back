package mercadolivre

import (
	"context"
	"errors"
	"testing"

	mldomain "vozko/domain/mercadolivre"
)

func TestBackgroundCallsTakeTheirOwnLowerShare(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, `{}`))
	limiters := newFakeLimiters()
	c := newTestClient(t, fs, limiters)
	if _, err := c.do(mldomain.WithBackgroundPriority(context.Background()), getRequest()); err != nil {
		t.Fatalf("do: %v", err)
	}
	got := limiters.keys()
	if len(got) != 3 || got[0] != "ml:415458330" || got[1] != BackgroundLimiterKey || got[2] != "ml:app:other" {
		t.Fatalf("limiter keys = %v", got)
	}
	budget := limiters.budgets[backgroundLimiterPrefix]
	if budget <= 0 {
		t.Fatalf("background budget = %d", budget)
	}
	for _, f := range Families() {
		if f == FamilyOAuth {
			continue
		}
		if budget >= familyBudgets[f] {
			t.Errorf("background share %d must stay below the %s budget %d so live traffic keeps headroom", budget, f, familyBudgets[f])
		}
	}
}

func TestLiveCallsSkipTheBackgroundLimiter(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, `{}`))
	limiters := newFakeLimiters()
	c := newTestClient(t, fs, limiters)
	if _, err := c.do(context.Background(), getRequest()); err != nil {
		t.Fatalf("do: %v", err)
	}
	for _, k := range limiters.keys() {
		if k == BackgroundLimiterKey {
			t.Fatal("live traffic must not spend the background share")
		}
	}
}

func TestABackgroundLimiterErrorRefusesTheCall(t *testing.T) {
	fs := newFakeServer(t, jsonReply(200, `{}`))
	limiters := newFakeLimiters()
	limiters.fail[BackgroundLimiterKey] = errors.New("redis down")
	c := newTestClient(t, fs, limiters)
	if _, err := c.do(mldomain.WithBackgroundPriority(context.Background()), getRequest()); err == nil || !IsRetryable(err) {
		t.Fatalf("err = %v, want a transient refusal", err)
	}
	if fs.count() != 0 {
		t.Fatal("a limiter error must never allow the call")
	}
}
