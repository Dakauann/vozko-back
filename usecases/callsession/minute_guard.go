package callsession_usecase

import (
	"time"

	"vozko/domain/calls/billing"
)

type minuteGuard struct {
	runner          *OutboundCallLifecycleRunner
	workspaceID     string
	callID          string
	perMinute       int64
	reservedMicros  int64
	reservedMinutes int64
	waves           billing.MinuteWaves
	failure         string
	next            *time.Timer
	stop            *time.Timer
}

func (r *OutboundCallLifecycleRunner) newMinuteGuard(workspaceID, callID string, perMinute, reservedMicros int64) *minuteGuard {
	return &minuteGuard{
		runner:          r,
		workspaceID:     workspaceID,
		callID:          callID,
		perMinute:       perMinute,
		reservedMicros:  reservedMicros,
		reservedMinutes: 1,
	}
}

func (g *minuteGuard) enabled() bool {
	return g.perMinute > 0 && g.runner.inflightReserver != nil && g.runner.cachedBalanceChecker != nil
}

func (g *minuteGuard) answered(at time.Time) {
	if !g.enabled() {
		return
	}
	g.waves = billing.MinuteWaves{AnsweredAt: at, Minute: g.runner.billingMinute, Lead: g.runner.reservationLead}
	g.scheduleNext(g.waves.NextReservationAt(g.reservedMinutes))
}

func (g *minuteGuard) nextC() <-chan time.Time {
	if g.next == nil {
		return nil
	}
	return g.next.C
}

func (g *minuteGuard) stopC() <-chan time.Time {
	if g.stop == nil {
		return nil
	}
	return g.stop.C
}

func (g *minuteGuard) reserveNextMinute() {
	g.next = nil
	if reason := g.reserve(); reason != "" {
		g.failure = reason
		coverageEnd := g.waves.CoverageEnd(g.reservedMinutes)
		if g.stop == nil {
			g.stop = g.runner.timerAt(coverageEnd)
		}
		if retryAt := g.runner.nowFn().Add(g.runner.reservationRetry); retryAt.Before(coverageEnd) {
			g.scheduleNext(retryAt)
		}
		return
	}
	g.reservedMicros += g.perMinute
	g.reservedMinutes++
	g.failure = ""
	if g.stop != nil {
		g.stop.Stop()
		g.stop = nil
	}
	g.scheduleNext(g.waves.NextReservationAt(g.reservedMinutes))
}

func (g *minuteGuard) reserve() string {
	budget, err := g.runner.cachedBalanceChecker.GetBalance(g.workspaceID)
	if err != nil {
		g.runner.logger.Printf("[CallBalanceGuard] call %s: balance read failed for ws %s: %v", g.callID, g.workspaceID, err)
		return endReasonBalanceCheckError
	}
	ok, err := g.runner.inflightReserver.Reserve(g.workspaceID, g.perMinute, budget)
	if err != nil {
		g.runner.logger.Printf("[CallBalanceGuard] call %s: reserving the next minute failed for ws %s: %v", g.callID, g.workspaceID, err)
		return endReasonBalanceCheckError
	}
	if !ok {
		g.runner.logger.Printf("[CallBalanceGuard] call %s: ws %s cannot cover minute %d (budget %d)", g.callID, g.workspaceID, g.reservedMinutes+1, budget)
		return endReasonInsufficientBalance
	}
	_ = g.runner.inflightReserver.RefreshTTL(g.workspaceID, inflightReservationTTL)
	return ""
}

func (g *minuteGuard) scheduleNext(at time.Time) {
	g.next = g.runner.timerAt(at)
}

func (g *minuteGuard) close() {
	if g.next != nil {
		g.next.Stop()
	}
	if g.stop != nil {
		g.stop.Stop()
	}
}

func (r *OutboundCallLifecycleRunner) timerAt(at time.Time) *time.Timer {
	delay := at.Sub(r.nowFn())
	if delay < 0 {
		delay = 0
	}
	return time.NewTimer(delay)
}
