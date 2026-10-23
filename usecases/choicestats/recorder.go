package choicestats_usecase

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"vozko/domain/choicestats"
)

var ErrRecorderUnwired = errors.New("choice stats: the recorder needs its store, the workspace zones, its metrics and the click tokens")

const zoneCacheTTL = 5 * time.Minute

type ZoneLocator interface {
	Location(ctx context.Context, workspaceID, departmentID string) (*time.Location, error)
}

type Metrics interface {
	IncChoiceStatsWrite(op, outcome string)
}

const (
	opSend      = "send"
	opClick     = "click"
	opLinkClick = "link_click"

	outcomeRecorded    = "recorded"
	outcomeDuplicate   = "duplicate"
	outcomeSkipped     = "skipped"
	outcomeFailed      = "failed"
	outcomeZoneUnknown = "zone_unknown"
)

type cachedZone struct {
	loc     *time.Location
	expires time.Time
}

type Recorder struct {
	store   choicestats.Store
	zones   ZoneLocator
	metrics Metrics
	tokens  *choicestats.ClickTokens
	now     func() time.Time

	mu    sync.Mutex
	cache map[string]cachedZone
}

var (
	_ choicestats.Recorder          = (*Recorder)(nil)
	_ choicestats.LinkClickRecorder = (*Recorder)(nil)
)

func NewRecorder(store choicestats.Store, zones ZoneLocator, metrics Metrics, tokens *choicestats.ClickTokens) (*Recorder, error) {
	if store == nil || zones == nil || metrics == nil || tokens == nil {
		return nil, ErrRecorderUnwired
	}
	return &Recorder{store: store, zones: zones, metrics: metrics, tokens: tokens, now: time.Now, cache: map[string]cachedZone{}}, nil
}

func (r *Recorder) RecordSend(ctx context.Context, send choicestats.Send) {
	ctx = context.WithoutCancel(ctx)
	loc, err := r.zone(ctx, send.WorkspaceID)
	if err != nil {
		r.outcome(opSend, outcomeZoneUnknown, slog.String("workspace_id", send.WorkspaceID), slog.String("prompt_key", send.PromptKey), slog.String("error", err.Error()))
		return
	}
	record, err := send.Record(loc)
	if err != nil {
		r.outcome(opSend, outcomeSkipped, slog.String("workspace_id", send.WorkspaceID), slog.String("prompt_key", send.PromptKey), slog.String("reason", err.Error()))
		return
	}
	isNew, err := r.store.RecordSend(ctx, record)
	switch {
	case err != nil:
		r.outcome(opSend, outcomeFailed, slog.String("workspace_id", send.WorkspaceID), slog.String("prompt_key", send.PromptKey), slog.String("error", err.Error()))
	case isNew:
		r.metrics.IncChoiceStatsWrite(opSend, outcomeRecorded)
	default:
		r.metrics.IncChoiceStatsWrite(opSend, outcomeDuplicate)
	}
}

func (r *Recorder) RecordClick(ctx context.Context, click choicestats.Click) {
	r.recordClick(context.WithoutCancel(ctx), opClick, click)
}

func (r *Recorder) RecordLinkClick(ctx context.Context, workspaceID, token string, at time.Time) {
	index, key, ok := r.tokens.Verify(token)
	if !ok {
		r.outcome(opLinkClick, outcomeSkipped, slog.String("workspace_id", workspaceID), slog.String("reason", "unverifiable click token"))
		return
	}
	r.recordClick(context.WithoutCancel(ctx), opLinkClick, choicestats.Click{
		WorkspaceID: workspaceID, PromptKey: key, Option: choicestats.URLOptionID(index), At: at,
	})
}

func (r *Recorder) recordClick(ctx context.Context, op string, click choicestats.Click) {
	record, err := click.Record()
	if err != nil {
		r.outcome(op, outcomeSkipped, slog.String("workspace_id", click.WorkspaceID), slog.String("reason", err.Error()))
		return
	}
	counted, err := r.store.RecordClick(ctx, record)
	switch {
	case err != nil:
		r.outcome(op, outcomeFailed, slog.String("workspace_id", click.WorkspaceID), slog.String("prompt_key", click.PromptKey), slog.String("error", err.Error()))
	case counted:
		r.metrics.IncChoiceStatsWrite(op, outcomeRecorded)
	default:
		r.metrics.IncChoiceStatsWrite(op, outcomeDuplicate)
	}
}

func (r *Recorder) outcome(op, outcome string, attrs ...any) {
	r.metrics.IncChoiceStatsWrite(op, outcome)
	level := slog.LevelInfo
	if outcome == outcomeFailed || outcome == outcomeZoneUnknown {
		level = slog.LevelWarn
	}
	slog.Log(context.Background(), level, "choice_stats."+op+"_"+outcome, attrs...)
}

func (r *Recorder) zone(ctx context.Context, workspaceID string) (*time.Location, error) {
	now := r.now()
	r.mu.Lock()
	cached, ok := r.cache[workspaceID]
	r.mu.Unlock()
	if ok && now.Before(cached.expires) {
		return cached.loc, nil
	}
	loc, err := r.zones.Location(ctx, workspaceID, "")
	if err != nil {
		return nil, err
	}
	if loc == nil {
		return nil, choicestats.ErrZoneUnknown
	}
	r.mu.Lock()
	r.cache[workspaceID] = cachedZone{loc: loc, expires: now.Add(zoneCacheTTL)}
	r.mu.Unlock()
	return loc, nil
}
