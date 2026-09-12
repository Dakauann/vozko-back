package audience_usecase

import (
	"context"
	"encoding/json"
	"log"
	"time"

	ca "vozko/domain/audience"
	"vozko/domain/cache"
	"vozko/domain/messaging"
	"vozko/domain/shared"
)

// Evaluating the alert rules after a batch is classified.
//
// It hangs off the same point the live feed does, for the same reason: that is
// where new facts land, already written. Everything here is best effort, and an
// alert that cannot be sent must never fail an analysis that was already paid
// for.
//
// Three guards, and each one exists because of a specific way this could go
// wrong in production:
//
//   - AlertFreshness stops a BACKFILL waking somebody up. Importing three
//     months of history classifies thousands of old comments; without this, a
//     comment from March would page someone tonight. Windowed metrics are
//     immune already (they count by occurred_at, so old rows fall outside the
//     window), but a per-comment rule would fire on every severe comment ever
//     written.
//   - One stats query per distinct WINDOW, not per rule. Three rules watching
//     the last hour ask once.
//   - The claim, in the repository, is what decides. ShouldFire only avoids
//     attempting a claim that would obviously lose.

// AlertFreshness is how recent a comment must be to trigger a per-comment
// alert. A day is generous for a live webhook and far short of any backfill.
const AlertFreshness = 24 * time.Hour

// AlertDeps groups what the evaluator needs. Any of them missing means alerts
// are simply not evaluated, which is how a deployment without a channel or
// without the table behaves.
type AlertDeps struct {
	Rules ca.AlertRuleRepository
	Repo  ca.Repository
	// Adapters resolve the post's public link and the account's handle. Without
	// them the alert still fires, it just cannot say where: the engine knows an
	// account by a workspace UUID and a post by a numeric id, and neither means
	// anything to the person being woken up.
	Adapters map[ca.Source]ca.SourceAdapter
	// Publisher hands a claimed alert to the sender. The briefing and the
	// outbound send are the two slow things an alert does, and this walk is
	// sequential across every workspace, so neither belongs on it.
	//
	// Optional. Without one, or when publishing fails, Sender is called inline:
	// a broker that is down makes alerts slow, never silent.
	Publisher messaging.MessageQueuePub
	Sender    AlertSender
	// State dedupes alerts per SUBJECT. Optional: without it a rule still
	// respects its cooldown and daily cap, it just cannot tell two bad
	// conversations from the same bad conversation twice.
	State cache.SharedState
	Clock ca.Clock
}

type alertEvaluator struct{ AlertDeps }

func NewAlertEvaluator(d AlertDeps) ca.AlertEvaluator { return &alertEvaluator{AlertDeps: d} }

func (e *alertEvaluator) EvaluateBatch(ctx context.Context, ref ca.ContainerRef, workspaceID string, rows []*ca.Analysis) {
	// Nowhere to hand an alert off to is the same as having no alerts: a
	// deployment with neither a queue nor a sender evaluates nothing rather
	// than claiming rules whose firings could never leave.
	if e.Rules == nil || (e.Publisher == nil && e.Sender == nil) || len(rows) == 0 {
		return
	}
	rules, err := e.Rules.ListArmed(ctx, ref.Source, ref.AccountID)
	if err != nil {
		log.Printf("[comment-analysis] alerts: listing rules for %s: %v", ref.AccountID, err)
		return
	}
	if len(rules) == 0 {
		return
	}

	now := e.now()
	// Windowed values are fetched lazily and shared: several rules commonly
	// watch the same span.
	windows := map[int]*ca.Stats{}
	var placeOnce *alertPlace

	// One selection per subject kind, reused across every rule that watches it:
	// a batch is one kind, so in practice this resolves once.
	subjects := map[ca.SubjectKind]*ca.Analysis{}

	for _, rule := range rules {
		if rule == nil || rule.WorkspaceID != workspaceID {
			continue
		}
		kind := rule.Metric.SubjectKind()
		worst, resolved := subjects[kind]
		if !resolved {
			worst = worstFresh(kind, rows, now)
			subjects[kind] = worst
		}
		// Is this subject worth judging at all? Asked before measuring, so a
		// conversation too short to mean anything costs nothing to reject.
		if !rule.Accepts(worst) {
			continue
		}
		value, observed := e.measure(ctx, rule, ref, worst, windows)
		if !observed || !rule.ShouldFire(value, now) {
			continue
		}
		// One subject must not consume the rule's whole daily cap. Without
		// this, a single conversation re-analysed every few minutes would send
		// every alert the rule is allowed to send that day, and the second
		// conversation to go wrong would be silently dropped.
		if !e.claimSubject(rule, worst) {
			continue
		}
		// Resolved lazily and once: a tick where nothing fires must not cost a
		// lookup, and several rules firing together share one.
		place := e.place(ctx, ref, &placeOnce)
		observation := ca.AlertObservation{
			Metric:      rule.Metric,
			Value:       value,
			AccountName: place.AccountName,
			Permalink:   place.Permalink,
			Comment:     commentFor(rule, worst),
		}
		e.fire(ctx, rule, observation, now)
	}
}

// alertPlace is what a human needs to be told about where this happened, and
// what the model needs to read it in context.
type alertPlace struct {
	AccountName string
	Permalink   string
	Caption     string
}

// place resolves the post and the account once per evaluation, and only once
// something is actually firing.
func (e *alertEvaluator) place(ctx context.Context, ref ca.ContainerRef, cache **alertPlace) alertPlace {
	if *cache != nil {
		return **cache
	}
	resolved := alertPlace{}
	if adapter, ok := e.Adapters[ref.Source]; ok && adapter != nil {
		if container, err := adapter.ReadContainerContext(ctx, ref); err == nil {
			resolved.Permalink = container.Permalink
			resolved.Caption = container.Caption
			resolved.AccountName = container.AccountName
		} else {
			log.Printf("[comment-analysis] alerts: reading container %s: %v", ref.ContainerID, err)
		}
	}
	*cache = &resolved
	return resolved
}

// measure returns the value a rule is watching, and whether it could be read at
// all. A windowed query that fails is "not observed" rather than zero, because
// zero is a real value that would fire an acceptance-score rule.
func (e *alertEvaluator) measure(
	ctx context.Context,
	rule *ca.AlertRule,
	ref ca.ContainerRef,
	worst *ca.Analysis,
	windows map[int]*ca.Stats,
) (int, bool) {
	if !rule.Metric.IsWindowed() {
		if worst == nil {
			return 0, false
		}
		switch rule.Metric {
		case ca.AlertMetricAttendanceQuality:
			return worst.AttendanceQuality, true
		default:
			return worst.Severity, true
		}
	}

	stats, ok := windows[rule.WindowMinutes]
	if !ok {
		var err error
		stats, err = e.windowStats(ctx, ref, rule)
		if err != nil {
			log.Printf("[comment-analysis] alerts: window stats for %s: %v", ref.AccountID, err)
			windows[rule.WindowMinutes] = nil
			return 0, false
		}
		windows[rule.WindowMinutes] = stats
	}
	if stats == nil {
		return 0, false
	}

	switch rule.Metric {
	case ca.AlertMetricHighSeverityCount:
		return stats.SeverityHighCount, true
	case ca.AlertMetricHostileCount:
		return stats.StanceHostile, true
	case ca.AlertMetricCommentVolume:
		return stats.Analyzed, true
	case ca.AlertMetricAcceptanceScore:
		// A window with nothing in it has no score to fall: reporting zero
		// would fire every acceptance rule on a quiet night.
		if stats.Analyzed == 0 {
			return 0, false
		}
		return stats.AcceptanceScore, true
	case ca.AlertMetricEscalationCount:
		return stats.NextActionEscalate, true
	}
	return 0, false
}

func (e *alertEvaluator) windowStats(ctx context.Context, ref ca.ContainerRef, rule *ca.AlertRule) (*ca.Stats, error) {
	from := e.now().Add(-rule.Window())
	in := ca.ListInput{
		WorkspaceID: rule.WorkspaceID,
		Source:      ref.Source,
		AccountID:   ref.AccountID,
		From:        &from,
		Statuses:    []ca.Status{ca.StatusAnalyzed},
		Options:     shared.QueryOptions{Pagination: shared.Pagination{Page: 1, PageSize: 1}},
	}
	in.Normalize()
	stats, err := e.Repo.GetStats(ctx, in)
	if err != nil {
		return nil, err
	}
	stats.Finalize()
	return stats, nil
}

// fire claims the rule and sends. The claim comes FIRST: losing it means
// another replica is already sending this, and sending anyway is the double
// message the whole design is trying to avoid.
func (e *alertEvaluator) fire(ctx context.Context, rule *ca.AlertRule, observation ca.AlertObservation, now time.Time) {
	claimed, err := e.Rules.ClaimFire(ctx, rule.WorkspaceID, rule.ID, now)
	if err != nil {
		log.Printf("[comment-analysis] alerts: claiming %s: %v", rule.ID, err)
		return
	}
	if !claimed {
		return
	}

	// The claim already happened, so this firing is owned: queueing it cannot
	// produce a second message, and the send is free to take as long as the
	// provider takes.
	//
	// The claim is also deliberately never released on failure. Releasing it
	// would retry on the next batch, which during an incident is seconds away,
	// and a channel that is down would then be hammered. The cooldown is the
	// retry interval, and the reason is stored where the operator looks.
	alert := ca.NewAlert(*rule, observation, now)
	if e.handOff(alert) {
		return
	}
	if e.Sender == nil {
		log.Printf("[comment-analysis] alerts: no sender for %s, alert dropped", rule.ID)
		return
	}
	_ = e.Sender.Send(ctx, alert)
}

// handOff queues the alert, reporting whether it got there. A broker that
// refuses is not a reason to lose an alert, so the caller sends it inline
// instead: slower, and the behaviour this had before the queue existed.
func (e *alertEvaluator) handOff(alert ca.Alert) bool {
	if e.Publisher == nil {
		return false
	}
	body, err := json.Marshal(alert)
	if err != nil {
		log.Printf("[comment-analysis] alerts: encoding %s: %v", alert.Rule.ID, err)
		return false
	}
	if err := e.Publisher.Publish(ca.TopicAlertSend, body); err != nil {
		log.Printf("[comment-analysis] alerts: queueing %s, sending inline instead: %v", alert.Rule.ID, err)
		return false
	}
	return true
}

// worstFresh is the RECENT row of the batch that a per-subject rule should be
// judged on. Recency is the backfill guard; "worst" is because a rule that
// fired on an arbitrary member of the batch would report a milder subject than
// the one that actually crossed the threshold.
//
// What "worst" means is the subject's own question: the most severe comment,
// the least well handled conversation.
func worstFresh(kind ca.SubjectKind, rows []*ca.Analysis, now time.Time) *ca.Analysis {
	var worst *ca.Analysis
	for _, row := range rows {
		if row == nil || row.Status != ca.StatusAnalyzed || row.SubjectKind != kind {
			continue
		}
		if now.Sub(row.OccurredAt) > AlertFreshness {
			continue
		}
		if worst == nil || worseFor(kind, row, worst) {
			worst = row
		}
	}
	return worst
}

func worseFor(kind ca.SubjectKind, candidate, incumbent *ca.Analysis) bool {
	if kind == ca.SubjectKindConversation {
		return candidate.AttendanceQuality < incumbent.AttendanceQuality
	}
	return candidate.Severity > incumbent.Severity
}

// claimSubject is the per-subject dedupe: one alert per rule per subject per
// AlertSubjectSuppression, on top of the rule's own cooldown and daily cap.
//
// Best effort. A store that is down must not silence alerts, so a failure reads
// as "not seen before" and the rule's cooldown and cap remain the backstop.
func (e *alertEvaluator) claimSubject(rule *ca.AlertRule, worst *ca.Analysis) bool {
	if e.State == nil || rule.Metric.IsWindowed() || worst == nil || worst.ID == "" {
		return true
	}
	first, err := e.State.SetNX("alert:subject:"+rule.ID+":"+worst.ID, "1", ca.AlertSubjectSuppression)
	if err != nil {
		log.Printf("[comment-analysis] alerts: subject dedupe unavailable for %s: %v", rule.ID, err)
		return true
	}
	return first
}

// commentFor attaches the comment only to a per-comment alert. A windowed alert
// has no single row behind it, and quoting one would tell the reader that this
// particular comment crossed a threshold it had nothing to do with.
func commentFor(rule *ca.AlertRule, worst *ca.Analysis) *ca.Analysis {
	if rule.Metric.IsWindowed() {
		return nil
	}
	return worst
}

func (e *alertEvaluator) now() time.Time {
	if e.Clock != nil {
		return e.Clock.Now()
	}
	return time.Now().UTC()
}
