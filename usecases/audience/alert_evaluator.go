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

const AlertFreshness = 24 * time.Hour

type AlertDeps struct {
	Rules     ca.AlertRuleRepository
	Repo      ca.Repository
	Adapters  map[ca.Source]ca.SourceAdapter
	Publisher messaging.MessageQueuePub
	Sender    AlertSender
	State     cache.SharedState
	Clock     ca.Clock
}

type alertEvaluator struct{ AlertDeps }

func NewAlertEvaluator(d AlertDeps) ca.AlertEvaluator { return &alertEvaluator{AlertDeps: d} }

func (e *alertEvaluator) EvaluateBatch(ctx context.Context, ref ca.ContainerRef, workspaceID string, rows []*ca.Analysis) {
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
	windows := map[int]*ca.Stats{}
	var placeOnce *alertPlace

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
		if !rule.Accepts(worst) {
			continue
		}
		value, observed := e.measure(ctx, rule, ref, worst, windows)
		if !observed || !rule.ShouldFire(value, now) {
			continue
		}
		if !e.claimSubject(rule, worst) {
			continue
		}
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

type alertPlace struct {
	AccountName string
	Permalink   string
	Caption     string
}

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
	if e.handOff(alert) {
		return
	}
	if e.Sender == nil {
		log.Printf("[comment-analysis] alerts: no sender for %s, alert dropped", rule.ID)
		return
	}
	_ = e.Sender.Send(ctx, alert)
}

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
