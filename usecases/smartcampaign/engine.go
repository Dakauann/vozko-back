package smartcampaign_usecase

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"vozko/domain/balance"
	"vozko/domain/campaign"
	"vozko/domain/whatsapp/template"
	wc "vozko/domain/whatsapp_campaign"
	sc "vozko/domain/whatsapp_campaign/smartcampaign"
	wce "vozko/domain/whatsapp_campaign_entry"
	"vozko/usecases/campaignqueue"
)

const (
	inFlightRecheck   = time.Minute
	healthRetry       = 5 * time.Minute
	lockTTL           = 30 * time.Second
	liveCheckDebounce = time.Minute
	dueBatch          = 200
)

type CampaignStore interface {
	FindByID(campaignID string) (*wc.Campaign, error)
	UpdateStatus(campaignID string, status wc.Status, allowed ...wc.Status) (bool, error)
}

type EntryCounter interface {
	CountByStatus(campaignID string) (*wce.StatusCounts, error)
}

type Consumer interface {
	SubscribeToCampaign(campaignID string) error
	PauseCampaignConsumer(campaignID string) error
	ResumeCampaignConsumer(campaignID string) error
	StopCampaignConsumer(campaignID string) error
	IsSubscribed(campaignID string) bool
}

type Enqueuer interface {
	Enqueue(campaignID string, messages []campaignqueue.Message) error
}

type HoldNotifier interface {
	Held(c *wc.Campaign, wave int, d sc.Decision, at time.Time)
	Resumed(c *wc.Campaign)
}

type Locks interface {
	SetNX(key, value string, ttl time.Duration) (bool, error)
	Del(keys ...string) error
}

type Deps struct {
	Campaigns CampaignStore
	Entries   EntryCounter
	Policies  sc.PolicyRepository
	States    sc.StateRepository
	Waves     sc.WaveRepository
	Health    sc.HealthReader
	Consumer  Consumer
	Queue     Enqueuer
	Caps      balance.MonthlySendCapUsageReader
	Notifier  HoldNotifier
	Locks     Locks
	Now       func() time.Time
}

type Engine struct {
	d Deps
}

func NewEngine(d Deps) *Engine {
	return &Engine{d: d}
}

type subject struct {
	campaign *wc.Campaign
	policy   sc.Policy
	state    sc.State
	current  *sc.Wave
}

type View struct {
	Policy   *sc.Policy  `json:"policy"`
	State    sc.State    `json:"state"`
	Waves    []sc.Wave   `json:"waves"`
	Progress sc.Progress `json:"progress"`
}

func (e *Engine) Manages(campaignID string) (bool, error) {
	policy, err := e.d.Policies.FindPolicy(campaignID)
	return policy != nil, err
}

func (e *Engine) Start(ctx context.Context, campaignID string) error {
	return e.locked(campaignID, func() error {
		s, err := e.load(campaignID)
		if err != nil {
			return err
		}
		if s.campaign.Status == campaign.StatusRunning {
			return campaign.ErrAlreadyRunning
		}
		blocking, err := e.blocking(ctx, s)
		if err != nil {
			return err
		}
		if blocking.Action.Holds() {
			return &sc.BlockedError{Decision: blocking}
		}
		if s.campaign.Status == campaign.StatusPaused && s.state.Hold == nil && s.current != nil && s.current.InFlight() {
			return e.continueWave(s)
		}
		if err := e.closeInterrupted(s); err != nil {
			return err
		}
		if err := e.swapStatus(s, campaign.StatusRunning, s.campaign.Status); err != nil {
			return err
		}
		s.state.Resumed(e.d.Now())
		e.d.Notifier.Resumed(s.campaign)
		if s.current != nil && s.current.State == sc.Drained && !s.current.ObservationOver(e.d.Now()) {
			s.state.DueAt = s.current.ObserveUntil
			return e.d.States.SaveState(s.state)
		}
		return e.evaluate(ctx, s, sc.BeforeNextWave)
	})
}

func (e *Engine) Tick(ctx context.Context) error {
	ids, err := e.d.States.DueCampaigns(e.d.Now(), dueBatch)
	if err != nil {
		return err
	}
	var errs []error
	for _, id := range ids {
		err := e.locked(id, func() error { return e.tickOne(ctx, id) })
		if err != nil && !errors.Is(err, sc.ErrCampaignBusy) {
			errs = append(errs, fmt.Errorf("smart campaign %s: %w", id, err))
		}
	}
	return errors.Join(errs...)
}

func (e *Engine) EntryFailed(ctx context.Context, campaignID string, code int) error {
	if !sc.IsSevere(code) {
		first, err := e.d.Locks.SetNX("smart_campaign:live:"+campaignID, "1", liveCheckDebounce)
		if err != nil || !first {
			return err
		}
	}
	managed, err := e.Manages(campaignID)
	if err != nil || !managed {
		return err
	}
	return e.liveCheck(ctx, campaignID)
}

func (e *Engine) HealthChanged(ctx context.Context, templateID, phoneID string) error {
	ids, err := e.d.States.RunningUsing(templateID, phoneID)
	if err != nil {
		return err
	}
	var errs []error
	for _, id := range ids {
		if err := e.liveCheck(ctx, id); err != nil {
			errs = append(errs, fmt.Errorf("smart campaign %s: %w", id, err))
		}
	}
	return errors.Join(errs...)
}

func (e *Engine) Overview(campaignID string) (*View, error) {
	policy, err := e.d.Policies.FindPolicy(campaignID)
	if err != nil {
		return nil, err
	}
	c, err := e.d.Campaigns.FindByID(campaignID)
	if err != nil {
		return nil, err
	}
	state, err := e.d.States.FindState(campaignID)
	if err != nil {
		return nil, err
	}
	waves, err := e.d.Waves.ListWaves(campaignID)
	if err != nil {
		return nil, err
	}
	var current *sc.Wave
	if n := len(waves); n > 0 {
		current = &waves[n-1]
		if !current.Judged() {
			if current.Outcome, err = e.d.Waves.WaveOutcome(campaignID, current.Number); err != nil {
				return nil, err
			}
		}
	}
	return &View{Policy: policy, State: state, Waves: waves, Progress: sc.Describe(c.Status, state, current)}, nil
}

func (e *Engine) liveCheck(ctx context.Context, campaignID string) error {
	return e.locked(campaignID, func() error {
		s, err := e.load(campaignID)
		if err != nil || s.campaign.Status != campaign.StatusRunning {
			return err
		}
		return e.evaluate(ctx, s, sc.DuringWave)
	})
}

func (e *Engine) tickOne(ctx context.Context, campaignID string) error {
	s, err := e.load(campaignID)
	if err != nil || s.campaign.Status != campaign.StatusRunning {
		return err
	}
	switch {
	case s.current == nil || s.current.Judged():
		return e.evaluate(ctx, s, sc.BeforeNextWave)
	case s.current.State == sc.Released:
		return e.retryEnqueue(s)
	case s.current.State == sc.Enqueued:
		return e.checkInFlight(ctx, s)
	case s.current.ObservationOver(e.d.Now()):
		return e.evaluate(ctx, s, sc.BeforeNextWave)
	default:
		s.state.DueAt = s.current.ObserveUntil
		return e.d.States.SaveState(s.state)
	}
}

func (e *Engine) checkInFlight(ctx context.Context, s *subject) error {
	if err := e.evaluate(ctx, s, sc.DuringWave); err != nil {
		return err
	}
	if s.campaign.Status != campaign.StatusRunning {
		return nil
	}
	pending, err := e.d.Waves.PendingInWave(s.campaign.ID, s.current.Number)
	if err != nil {
		return err
	}
	now := e.d.Now()
	if pending > 0 {
		s.state.DueAt = now.Add(inFlightRecheck)
		return e.d.States.SaveState(s.state)
	}
	if err := s.current.MarkDrained(now, s.policy.ObserveWait); err != nil {
		return err
	}
	if err := e.d.Waves.SaveWave(*s.current); err != nil {
		return err
	}
	s.state.DueAt = s.current.ObserveUntil
	return e.d.States.SaveState(s.state)
}

func (e *Engine) evaluate(ctx context.Context, s *subject, m sc.Moment) error {
	outcome, err := e.currentOutcome(s)
	if err != nil {
		return err
	}
	snapshot, err := e.snapshot(ctx, s, false, outcome, m)
	if err != nil {
		return err
	}
	history, err := e.history(s, m)
	if err != nil {
		return err
	}
	d := sc.Decide(s.policy, snapshot, history, m)
	if m == sc.BeforeNextWave {
		if d.Action.Releases() {
			if snapshot, err = e.snapshot(ctx, s, true, outcome, m); err != nil {
				return err
			}
		}
		s.state.TrackHealthReads(len(snapshot.Unread) > 0, snapshot.Now)
		history.UnreadSince = s.state.UnreadSince
		d = sc.Decide(s.policy, snapshot, history, m)
	}
	return e.apply(s, outcome, d, m)
}

func (e *Engine) apply(s *subject, outcome sc.Outcome, d sc.Decision, m sc.Moment) error {
	switch {
	case d.Action.Holds():
		return e.hold(s, outcome, d)
	case m == sc.DuringWave:
		return nil
	case d.Action == sc.Wait:
		return e.wait(s, d)
	case d.Action == sc.Complete:
		return e.complete(s, outcome, d)
	default:
		return e.advance(s, outcome, d)
	}
}

func (e *Engine) hold(s *subject, outcome sc.Outcome, d sc.Decision) error {
	now := e.d.Now()
	if err := e.detach(s.campaign.ID, d.Action); err != nil {
		s.state.DueAt = now.Add(inFlightRecheck)
		return errors.Join(fmt.Errorf("detach the consumer: %w", err), e.d.States.SaveState(s.state))
	}
	target, allowed := campaign.StatusPaused, []wc.Status{campaign.StatusRunning}
	if d.Action == sc.Stop {
		target, allowed = campaign.StatusStopped, []wc.Status{campaign.StatusRunning, campaign.StatusPaused}
	}
	swapped, err := e.d.Campaigns.UpdateStatus(s.campaign.ID, target, allowed...)
	if err != nil || !swapped {
		return err
	}
	s.campaign.Status = target
	if err := e.judge(s, outcome, d); err != nil {
		return err
	}
	s.state.Held(d, now)
	if err := e.d.States.SaveState(s.state); err != nil {
		return err
	}
	wave := 0
	if s.current != nil {
		wave = s.current.Number
	}
	e.d.Notifier.Held(s.campaign, wave, d, now)
	return nil
}

func (e *Engine) detach(campaignID string, action sc.Action) error {
	if action == sc.Stop {
		return e.d.Consumer.StopCampaignConsumer(campaignID)
	}
	return e.d.Consumer.PauseCampaignConsumer(campaignID)
}

func (e *Engine) wait(s *subject, d sc.Decision) error {
	due := d.WaitUntil
	if due.IsZero() {
		due = e.d.Now().Add(healthRetry)
	}
	s.state.WaitFor(d, due)
	return e.d.States.SaveState(s.state)
}

func (e *Engine) complete(s *subject, outcome sc.Outcome, d sc.Decision) error {
	if err := e.judge(s, outcome, d); err != nil {
		return err
	}
	if _, err := e.d.Campaigns.UpdateStatus(s.campaign.ID, campaign.StatusCompleted, campaign.StatusRunning); err != nil {
		return err
	}
	s.state.Waiting = nil
	s.state.DueAt = time.Time{}
	return e.d.States.SaveState(s.state)
}

func (e *Engine) advance(s *subject, outcome sc.Outcome, d sc.Decision) error {
	next := 1
	if s.current != nil {
		next = s.current.Number + 1
		if err := e.judge(s, outcome, d); err != nil {
			return err
		}
	}
	released, err := e.d.Waves.ClaimWave(s.campaign.ID, next, d.NextSize, e.d.Now())
	if errors.Is(err, sc.ErrAlreadyReleased) {
		return nil
	}
	if err != nil {
		return err
	}
	s.current = &released
	s.state.WaveReleased(e.d.Now().Add(inFlightRecheck))
	if err := e.d.States.SaveState(s.state); err != nil {
		return err
	}
	return e.enqueue(s)
}

func (e *Engine) retryEnqueue(s *subject) error {
	s.state.DueAt = e.d.Now().Add(inFlightRecheck)
	if err := e.d.States.SaveState(s.state); err != nil {
		return err
	}
	return e.enqueue(s)
}

func (e *Engine) enqueue(s *subject) error {
	recipients, err := e.d.Waves.WaveRecipients(s.campaign.ID, s.current.Number)
	if err != nil {
		return err
	}
	messages := make([]campaignqueue.Message, 0, len(recipients))
	for _, r := range recipients {
		messages = append(messages, campaignqueue.Message{CampaignID: s.campaign.ID, EntryID: r.EntryID, PhoneNumber: r.PhoneNumber})
	}
	if err := e.d.Consumer.SubscribeToCampaign(s.campaign.ID); err != nil {
		return err
	}
	if err := e.d.Queue.Enqueue(s.campaign.ID, messages); err != nil {
		return err
	}
	if err := s.current.MarkEnqueued(); err != nil {
		return err
	}
	return e.d.Waves.SaveWave(*s.current)
}

func (e *Engine) judge(s *subject, outcome sc.Outcome, d sc.Decision) error {
	if s.current == nil || s.current.Judged() {
		return nil
	}
	s.current.MarkDecided(e.d.Now(), outcome, d)
	return e.d.Waves.SaveWave(*s.current)
}

func (e *Engine) continueWave(s *subject) error {
	if err := e.swapStatus(s, campaign.StatusRunning, campaign.StatusPaused); err != nil {
		return err
	}
	attach := e.d.Consumer.SubscribeToCampaign
	if e.d.Consumer.IsSubscribed(s.campaign.ID) {
		attach = e.d.Consumer.ResumeCampaignConsumer
	}
	if err := attach(s.campaign.ID); err != nil {
		_, revertErr := e.d.Campaigns.UpdateStatus(s.campaign.ID, campaign.StatusPaused, campaign.StatusRunning)
		return errors.Join(err, revertErr)
	}
	s.state.Resumed(e.d.Now())
	e.d.Notifier.Resumed(s.campaign)
	s.state.DueAt = e.d.Now().Add(inFlightRecheck)
	return e.d.States.SaveState(s.state)
}

func (e *Engine) closeInterrupted(s *subject) error {
	if s.current == nil {
		return nil
	}
	if s.current.InFlight() {
		outcome, err := e.d.Waves.WaveOutcome(s.campaign.ID, s.current.Number)
		if err != nil {
			return err
		}
		if err := e.judge(s, outcome, sc.Decision{Action: interruptedBy(s.campaign.Status)}); err != nil {
			return err
		}
	}
	pending, err := e.d.Waves.PendingInWave(s.campaign.ID, s.current.Number)
	if err != nil || pending == 0 {
		return err
	}
	if err := e.d.Consumer.StopCampaignConsumer(s.campaign.ID); err != nil {
		return err
	}
	_, err = e.d.Waves.ReturnToPool(s.campaign.ID, s.current.Number)
	return err
}

func interruptedBy(status wc.Status) sc.Action {
	if status == campaign.StatusPaused {
		return sc.Pause
	}
	return sc.Stop
}

func (e *Engine) swapStatus(s *subject, target wc.Status, from wc.Status) error {
	swapped, err := e.d.Campaigns.UpdateStatus(s.campaign.ID, target, from)
	if err != nil {
		return err
	}
	if !swapped {
		return campaign.SwapFailure(campaign.ActionStart)
	}
	s.campaign.Status = target
	return nil
}

func (e *Engine) blocking(ctx context.Context, s *subject) (sc.Decision, error) {
	snapshot, err := e.snapshot(ctx, s, true, sc.Outcome{}, sc.DuringWave)
	if err != nil {
		return sc.Decision{}, err
	}
	return sc.Decide(s.policy, snapshot, sc.History{}, sc.DuringWave), nil
}

func (e *Engine) currentOutcome(s *subject) (sc.Outcome, error) {
	if s.current == nil || s.current.Judged() {
		return sc.Outcome{}, nil
	}
	return e.d.Waves.WaveOutcome(s.campaign.ID, s.current.Number)
}

func (e *Engine) history(s *subject, m sc.Moment) (sc.History, error) {
	if m == sc.DuringWave {
		return sc.History{}, nil
	}
	waves, err := e.d.Waves.ListWaves(s.campaign.ID)
	if err != nil {
		return sc.History{}, err
	}
	size := 0
	if s.current != nil && s.current.Decision.Action != sc.Complete {
		size = s.current.Size
	}
	h := s.state.History(size, sc.SlowStreakAtFloor(waves, s.policy.MinWaveSize))
	h.Interrupted = s.current != nil && s.current.Judged() && s.current.Decision.Action.Holds()
	return h, nil
}

func (e *Engine) snapshot(ctx context.Context, s *subject, fresh bool, outcome sc.Outcome, m sc.Moment) (sc.Snapshot, error) {
	now := e.d.Now()
	snapshot := sc.Snapshot{Now: now, Wave: outcome}
	if tmpl, err := e.d.Health.Template(ctx, s.campaign.TemplateID); err != nil {
		snapshot.Unread = append(snapshot.Unread, sc.SignalTemplate)
	} else {
		tmpl.CreatedAs = template.TemplateCategory(strings.ToUpper(strings.TrimSpace(s.campaign.TemplateCategory)))
		snapshot.Template = tmpl
	}
	if phone, err := e.d.Health.Phone(ctx, s.campaign.BusinessPhoneID, fresh); err != nil {
		snapshot.Unread = append(snapshot.Unread, sc.SignalPhone)
	} else {
		snapshot.Phone = phone
	}
	if m == sc.DuringWave {
		return snapshot, nil
	}
	counts, err := e.d.Entries.CountByStatus(s.campaign.ID)
	if err != nil {
		return snapshot, err
	}
	if snapshot.CapRoom, err = e.capRoom(s.campaign.WorkspaceID, now); err != nil {
		return snapshot, err
	}
	if snapshot.WavesToday, err = e.d.Waves.ReleasedSince(s.campaign.ID, s.policy.DayStart(now)); err != nil {
		return snapshot, err
	}
	snapshot.Pending = int(counts.Pending)
	return snapshot, nil
}

func (e *Engine) capRoom(workspaceID string, now time.Time) (int, error) {
	usage, err := e.d.Caps.MonthlySendCapUsage(workspaceID, now)
	if err != nil {
		return 0, err
	}
	if usage == nil {
		return math.MaxInt32, nil
	}
	return int(max(0, min(usage.Remaining(), math.MaxInt32))), nil
}

func (e *Engine) load(campaignID string) (*subject, error) {
	policy, err := e.d.Policies.FindPolicy(campaignID)
	if err != nil {
		return nil, err
	}
	if policy == nil {
		return nil, sc.ErrNotSmart
	}
	c, err := e.d.Campaigns.FindByID(campaignID)
	if err != nil {
		return nil, err
	}
	state, err := e.d.States.FindState(campaignID)
	if err != nil {
		return nil, err
	}
	current, err := e.d.Waves.CurrentWave(campaignID)
	if err != nil {
		return nil, err
	}
	return &subject{campaign: c, policy: *policy, state: state, current: current}, nil
}

func (e *Engine) locked(campaignID string, run func() error) error {
	key := "smart_campaign:lock:" + campaignID
	acquired, err := e.d.Locks.SetNX(key, "1", lockTTL)
	if err != nil {
		return err
	}
	if !acquired {
		return sc.ErrCampaignBusy
	}
	defer func() { _ = e.d.Locks.Del(key) }()
	return run()
}
