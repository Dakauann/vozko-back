package audience_usecase

import (
	"context"
	"log"
	"time"

	"vozko/domain/balance"
	ca "vozko/domain/audience"
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
	Rules      ca.AlertRuleRepository
	Repo       ca.Repository
	Dispatcher ca.AlertDispatcher
	// Adapters resolve the post's public link and the account's handle. Without
	// them the alert still fires, it just cannot say where: the engine knows an
	// account by a workspace UUID and a post by a numeric id, and neither means
	// anything to the person being woken up.
	Adapters map[ca.Source]ca.SourceAdapter
	// Briefer writes the model's reading of what happened. Optional, opt-in per
	// rule, and skipped silently on any error.
	Briefer  ca.AlertBriefer
	Settings ca.SettingsRepository
	// Balance and Batches put the briefing under the same floor and on the same
	// spend page as every other model call in the engine.
	Balance balance.CachedBalanceChecker
	Batches ca.BatchRepository
	Clock   ca.Clock
}

type alertEvaluator struct {
	AlertDeps
	guard balanceGuard
}

func NewAlertEvaluator(d AlertDeps) ca.AlertEvaluator {
	return &alertEvaluator{AlertDeps: d, guard: newBalanceGuard(d.Balance, "alert briefing")}
}

func (e *alertEvaluator) EvaluateBatch(ctx context.Context, ref ca.ContainerRef, workspaceID string, rows []*ca.Analysis) {
	if e.Rules == nil || e.Dispatcher == nil || len(rows) == 0 {
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
	worst := worstFreshComment(rows, now)
	// Windowed values are fetched lazily and shared: several rules commonly
	// watch the same span.
	windows := map[int]*ca.Stats{}
	var placeOnce *alertPlace

	for _, rule := range rules {
		if rule == nil || rule.WorkspaceID != workspaceID {
			continue
		}
		value, observed := e.measure(ctx, rule, ref, worst, windows)
		if !observed || !rule.ShouldFire(value, now) {
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
		if rule.Brief {
			observation.Briefing = e.brief(ctx, rule, observation, place.Caption)
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

// brief asks the model to read the alert. Never fatal: an alert without advice
// still tells somebody something is wrong, and one that did not arrive tells
// them nothing.
func (e *alertEvaluator) brief(ctx context.Context, rule *ca.AlertRule, obs ca.AlertObservation, caption string) ca.AlertBriefing {
	if e.Briefer == nil {
		return ca.AlertBriefing{}
	}
	// The alert itself is free and always goes out; only the briefing is a
	// model call, so only the briefing answers to the balance floor.
	if err := e.guard.Allow(rule.WorkspaceID); err != nil {
		return ca.AlertBriefing{}
	}
	req := ca.AlertBriefRequest{
		WorkspaceID: rule.WorkspaceID,
		Caption:     caption,
		RuleName:    rule.Name,
		Measurement: ca.NewAlert(*rule, obs, e.now()).Headline(),
	}
	if c := obs.Comment; c != nil {
		req.Comment, req.Stance, req.Severity = c.Excerpt, c.Stance, c.Severity
	}
	if e.Settings != nil {
		if s, err := e.Settings.Find(ctx, rule.Source, rule.AccountID); err == nil && s != nil {
			req.Model, req.Instructions = s.Model, s.Instructions
		}
	}

	briefing, err := e.Briefer.Brief(ctx, req)
	if err != nil || briefing == nil {
		if err != nil {
			log.Printf("[comment-analysis] alerts: briefing %s: %v", rule.ID, err)
		}
		return ca.AlertBriefing{}
	}
	recordAICall(ctx, e.Batches, e.Clock, aiCall{
		WorkspaceID: rule.WorkspaceID, Source: rule.Source, AccountID: rule.AccountID,
		Kind: ca.BatchKindAlertBrief, Model: briefing.Model,
		PromptTokens: briefing.PromptTokens, CompletionTokens: briefing.CompletionTokens,
	})
	briefing.Normalize()
	return *briefing
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
		return worst.Severity, true
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

	alert := ca.NewAlert(*rule, observation, now)
	if err := e.Dispatcher.Dispatch(ctx, ca.AlertDelivery{
		WorkspaceID:     rule.WorkspaceID,
		Channel:         rule.Channel,
		Recipient:       rule.Recipient,
		BusinessPhoneID: rule.BusinessPhoneID,
		TemplateID:      rule.TemplateID,
		TemplateParams:  alert.TemplateParams(),
		Facts:           alert.Facts(),
		InstanceID:      rule.InstanceID,
		Text:            alert.Message(),
		IdempotencyKey:  alert.IdempotencyKey(),
		ActorUserID:     rule.CreatedByUserID,
	}); err != nil {
		// The claim is deliberately NOT released. Releasing it would retry on
		// the next batch, which during an incident is seconds away, and a
		// channel that is down would then be hammered. The cooldown is the
		// retry interval, and the reason is stored so the operator can see it.
		log.Printf("[comment-analysis] alerts: dispatching %s: %v", rule.ID, err)
		if err := e.Rules.RecordFailure(ctx, rule.WorkspaceID, rule.ID, err.Error(), now); err != nil {
			log.Printf("[comment-analysis] alerts: recording failure for %s: %v", rule.ID, err)
		}
	}
}

// worstFreshComment is the most severe RECENT comment of the batch. Recency is
// the backfill guard; severity is because a per-comment rule that fired on an
// arbitrary member of the batch would report a milder comment than the one that
// crossed the threshold.
func worstFreshComment(rows []*ca.Analysis, now time.Time) *ca.Analysis {
	var worst *ca.Analysis
	for _, row := range rows {
		if row == nil || row.Status != ca.StatusAnalyzed {
			continue
		}
		if now.Sub(row.OccurredAt) > AlertFreshness {
			continue
		}
		if worst == nil || row.Severity > worst.Severity {
			worst = row
		}
	}
	return worst
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
