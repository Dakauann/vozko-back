package campaignqueue

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"vozko/domain/cache"
	"vozko/domain/campaign"
	"vozko/domain/messaging"
)

const remainingCounterTTL = 72 * time.Hour

type Message struct {
	CampaignID  string `json:"campaignId"`
	EntryID     string `json:"entryId"`
	PhoneNumber string `json:"phoneNumber,omitempty"`
}

type Outcome int

const (
	OutcomeDone Outcome = iota
	OutcomeDrop
	OutcomeRetryLater
	OutcomeRequeue
)

type Result struct {
	Outcome Outcome
	Delay   time.Duration
}

var (
	Done    = Result{Outcome: OutcomeDone}
	Drop    = Result{Outcome: OutcomeDrop}
	Requeue = Result{Outcome: OutcomeRequeue}
)

func RetryLater(d time.Duration) Result {
	return Result{Outcome: OutcomeRetryLater, Delay: d}
}

type Handler func(Message) Result

type StatusStore interface {
	ListRunningCampaignIDs() ([]string, error)
	CompleteCampaign(campaignID string) (bool, error)
}

type PendingCounter interface {
	CountPendingEntries(campaignID string) (int64, error)
}

type Config struct {
	Namespace campaign.Namespace

	PauseRequeueDelay time.Duration

	Precheck func(campaignID string) error

	Pace func(campaignID string) time.Duration

	Logf func(format string, args ...any)
}

func (c Config) logf(format string, args ...any) {
	if c.Logf != nil {
		c.Logf(format, args...)
		return
	}
	log.Printf(format, args...)
}

type Runner struct {
	sub     messaging.MessageQueueSub
	pub     messaging.MessageQueuePub
	shared  cache.SharedState
	status  StatusStore
	pending PendingCounter
	cfg     Config
	handle  Handler

	mu         sync.RWMutex
	subscribed map[string]bool
	paused     map[string]bool
}

func New(
	sub messaging.MessageQueueSub,
	pub messaging.MessageQueuePub,
	sharedState cache.SharedState,
	status StatusStore,
	pending PendingCounter,
	cfg Config,
	handle Handler,
) *Runner {
	return &Runner{
		sub:        sub,
		pub:        pub,
		shared:     sharedState,
		status:     status,
		pending:    pending,
		cfg:        cfg,
		handle:     handle,
		subscribed: make(map[string]bool),
		paused:     make(map[string]bool),
	}
}

func (r *Runner) Start() error {
	if r.sub == nil {
		return fmt.Errorf("campaign queue: message queue subscriber is required")
	}

	running, err := r.status.ListRunningCampaignIDs()
	if err != nil {
		return fmt.Errorf("campaign queue: failed to list active campaigns: %w", err)
	}

	var wg sync.WaitGroup
	errs := make([]error, len(running))
	for i, id := range running {
		wg.Add(1)
		go func(idx int, campaignID string) {
			defer wg.Done()
			if r.queueIsEmpty(campaignID) {
				if done, err := r.status.CompleteCampaign(campaignID); err == nil && done {
					r.cfg.logf("campaign queue: campaign %s completed on startup (queue empty)", campaignID)
				}
				return
			}
			r.SeedCounterIfMissing(campaignID)
			if err := r.SubscribeToCampaign(campaignID); err != nil {
				errs[idx] = fmt.Errorf("campaign queue: failed to subscribe to campaign %s: %w", campaignID, err)
			}
		}(i, id)
	}
	wg.Wait()

	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

func (r *Runner) SubscribeToCampaign(campaignID string) error {
	r.mu.RLock()
	already := r.subscribed[campaignID]
	r.mu.RUnlock()
	if already {
		return nil
	}

	_ = r.shared.Del(
		r.cfg.Namespace.StoppedKey(campaignID),
		r.cfg.Namespace.PausedKey(campaignID),
	)

	if r.cfg.Precheck != nil {
		if err := r.cfg.Precheck(campaignID); err != nil {
			return err
		}
	}

	topic := r.cfg.Namespace.DispatchTopic(campaignID)
	err := r.sub.Subscribe(topic, func(raw []byte, ack messaging.MessageAck) {
		r.deliver(topic, raw, ack)
	})
	if err != nil {
		return fmt.Errorf("failed to subscribe to campaign %s: %w", campaignID, err)
	}

	r.mu.Lock()
	r.subscribed[campaignID] = true
	delete(r.paused, campaignID)
	r.mu.Unlock()

	r.cfg.logf("campaign queue: subscribed to campaign %s", campaignID)
	return nil
}

func (r *Runner) deliver(topic string, raw []byte, ack messaging.MessageAck) {
	var msg Message
	if err := json.Unmarshal(raw, &msg); err != nil {
		r.cfg.logf("campaign queue: failed to decode dispatch payload: %v", err)
		_ = ack.Nack(false)
		return
	}

	if held, _ := r.shared.Exists(r.cfg.Namespace.PausedKey(msg.CampaignID)); held {
		r.republish(topic, raw, r.cfg.PauseRequeueDelay)
		_ = ack.Ack()
		return
	}
	if stopped, _ := r.shared.Exists(r.cfg.Namespace.StoppedKey(msg.CampaignID)); stopped {
		_ = ack.Ack()
		return
	}

	result := r.handle(msg)

	switch result.Outcome {
	case OutcomeRequeue:
		_ = ack.Nack(true)
		return

	case OutcomeRetryLater:
		r.republish(topic, raw, result.Delay)
		_ = ack.Ack()
		return

	case OutcomeDone, OutcomeDrop:
		if err := ack.Ack(); err != nil {
			r.cfg.logf("campaign queue: failed to ack %s/%s: %v", msg.CampaignID, msg.EntryID, err)
		}
		if result.Outcome == OutcomeDone && r.cfg.Pace != nil {
			if d := r.cfg.Pace(msg.CampaignID); d > 0 {
				time.Sleep(d)
			}
		}
		r.countProcessed(msg.CampaignID, topic)
	}
}

func (r *Runner) PauseCampaignConsumer(campaignID string) error {
	if err := r.shared.SetString(r.cfg.Namespace.PausedKey(campaignID), "1", 0); err != nil {
		return err
	}

	r.mu.RLock()
	subscribed := r.subscribed[campaignID]
	r.mu.RUnlock()
	if !subscribed {
		return nil
	}

	stopper, ok := r.sub.(interface{ StopConsumer(string) error })
	if !ok {
		return nil
	}
	if err := stopper.StopConsumer(r.cfg.Namespace.DispatchTopic(campaignID)); err != nil {
		return fmt.Errorf("failed to pause campaign %s: %w", campaignID, err)
	}

	r.mu.Lock()
	delete(r.subscribed, campaignID)
	r.paused[campaignID] = true
	r.mu.Unlock()
	return nil
}

func (r *Runner) ResumeCampaignConsumer(campaignID string) error {
	if err := r.shared.Del(r.cfg.Namespace.PausedKey(campaignID)); err != nil {
		return err
	}

	r.mu.Lock()
	wasPaused := r.paused[campaignID]
	delete(r.paused, campaignID)
	r.mu.Unlock()
	if !wasPaused {
		return nil
	}
	return r.SubscribeToCampaign(campaignID)
}

func (r *Runner) StopCampaignConsumer(campaignID string) error {
	ns := r.cfg.Namespace
	_ = r.shared.SetString(ns.StoppedKey(campaignID), "1", 0)
	_ = r.shared.Del(ns.PausedKey(campaignID), ns.RemainingKey(campaignID))

	if err := r.sub.DeleteQueue(ns.DispatchTopic(campaignID)); err != nil {
		return fmt.Errorf("failed to delete queue for campaign %s: %w", campaignID, err)
	}

	r.mu.Lock()
	delete(r.subscribed, campaignID)
	delete(r.paused, campaignID)
	r.mu.Unlock()

	r.cfg.logf("campaign queue: stopped and deleted queue for campaign %s", campaignID)
	return nil
}

func (r *Runner) IsSubscribed(campaignID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.subscribed[campaignID]
}

func (r *Runner) SetCounter(campaignID string, n int) {
	if n <= 0 {
		return
	}
	_ = r.shared.SetString(r.cfg.Namespace.RemainingKey(campaignID), strconv.Itoa(n), remainingCounterTTL)
}

func (r *Runner) SeedCounterIfMissing(campaignID string) {
	key := r.cfg.Namespace.RemainingKey(campaignID)
	if exists, err := r.shared.Exists(key); err != nil || exists {
		return
	}
	pending, err := r.pending.CountPendingEntries(campaignID)
	if err != nil || pending == 0 {
		return
	}
	_ = r.shared.SetString(key, strconv.FormatInt(pending, 10), remainingCounterTTL)
}

func (r *Runner) countProcessed(campaignID, topic string) {
	key := r.cfg.Namespace.RemainingKey(campaignID)
	remaining, err := r.shared.Decr(key)
	if err != nil || remaining != 0 {
		return
	}

	done, err := r.status.CompleteCampaign(campaignID)
	if err != nil || !done {
		return
	}

	r.cfg.logf("campaign queue: campaign %s completed (all entries processed)", campaignID)

	_ = r.shared.Del(r.cfg.Namespace.PausedKey(campaignID), key)
	_ = r.sub.DeleteQueue(topic)

	r.mu.Lock()
	delete(r.subscribed, campaignID)
	r.mu.Unlock()
}

func (r *Runner) republish(topic string, raw []byte, delay time.Duration) {
	if err := r.pub.PublishWithDelay(topic, raw, delay); err != nil {
		r.cfg.logf("campaign queue: failed to requeue on topic %s: %v", topic, err)
	}
}

func (r *Runner) queueIsEmpty(campaignID string) bool {
	topic := r.cfg.Namespace.DispatchTopic(campaignID)
	mainLen, err := r.sub.GetQueueLength(topic)
	if err != nil || mainLen > 0 {
		return false
	}
	delayLen, err := r.sub.GetQueueLength(topic + messaging.DelayQueueSuffix)
	return err == nil && delayLen == 0
}
