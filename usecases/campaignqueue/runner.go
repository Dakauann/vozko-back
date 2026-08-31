// Package campaignqueue is the queue machinery every campaign needs, on any
// channel.
//
// A campaign consumer does two very different jobs. One is bookkeeping — own a
// queue per campaign, know which campaigns are subscribed, honour pause and
// stop, requeue rather than drop when the campaign is held, and notice when the
// last entry has been processed. The other is the send itself, which is entirely
// about the transport: a template and a balance debit on the Cloud API, a
// session and a pacing budget on a linked device.
//
// Only the second is channel-specific. The first was ~200 lines inside the Cloud
// API consumer, and copying it for a second channel would have meant two
// implementations of "is this campaign finished" — a question that is already
// subtle enough to get wrong once.
//
// So the bookkeeping lives here and the send is a callback. A channel supplies a
// Handler and gets pause, resume, stop, restart-on-boot and completion for free.
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

// remainingCounterTTL bounds how long a completion counter outlives its
// campaign.
//
// Long enough that a campaign paused overnight still completes correctly, short
// enough that an abandoned one does not hold a key forever. A counter that
// expires mid-campaign is recovered by SeedCounterIfMissing on the next
// subscribe.
const remainingCounterTTL = 72 * time.Hour

// Message is one unit of campaign work.
//
// The wire shape is shared: a channel that invented its own would also have to
// own encoding, decoding and the version skew between a queued message and the
// consumer that reads it after a deploy.
type Message struct {
	CampaignID  string `json:"campaignId"`
	EntryID     string `json:"entryId"`
	PhoneNumber string `json:"phoneNumber,omitempty"`
}

// Outcome is what the channel's handler decided about one message.
//
// Naming the four decisions is what keeps ack/nack ordering out of the channel
// callbacks. In the Cloud API consumer this logic was spelled out at eleven
// separate return sites, and the difference between "ack and count it" and "ack
// and do NOT count it" is the difference between a campaign that completes and
// one that hangs at 99%.
type Outcome int

const (
	// OutcomeDone: the send happened. Ack, count it, then pace.
	OutcomeDone Outcome = iota
	// OutcomeDrop: no send happened and none ever will for this entry (it was
	// skipped, or it failed terminally). Ack and count it — the entry is
	// resolved, so the campaign is one closer to finished — but do not pace,
	// because no message left and there is nothing to space out.
	OutcomeDrop
	// OutcomeRetryLater: the campaign is held (paused, throttled, out of
	// budget). Republish with a delay and ack the original. Deliberately does
	// NOT count: the entry is still pending and counting it would complete the
	// campaign while work remains.
	OutcomeRetryLater
	// OutcomeRequeue: a transient infrastructure fault. Nack with requeue and
	// let the broker redeliver.
	OutcomeRequeue
)

// Result is an Outcome plus, for OutcomeRetryLater, how long to wait.
type Result struct {
	Outcome Outcome
	Delay   time.Duration
}

var (
	Done    = Result{Outcome: OutcomeDone}
	Drop    = Result{Outcome: OutcomeDrop}
	Requeue = Result{Outcome: OutcomeRequeue}
)

// RetryLater holds this entry and tries again after d.
func RetryLater(d time.Duration) Result {
	return Result{Outcome: OutcomeRetryLater, Delay: d}
}

// Handler is the channel's send step.
type Handler func(Message) Result

// StatusStore is the slice of a campaign repository the runner needs.
//
// Two methods, not the whole repository, so a channel's repository is not
// dragged into this package and a test double is three lines.
type StatusStore interface {
	// ListRunningCampaignIDs powers restart-on-boot.
	ListRunningCampaignIDs() ([]string, error)
	// CompleteCampaign moves RUNNING to COMPLETED, reporting whether it changed
	// anything. Compare-and-swap, so two replicas finishing the last two entries
	// at once cannot both declare completion.
	CompleteCampaign(campaignID string) (bool, error)
}

// PendingCounter reports how many entries are still to process, used to rebuild
// a completion counter that expired or was never seeded.
type PendingCounter interface {
	CountPendingEntries(campaignID string) (int64, error)
}

// Config is what one channel's runner needs to know about itself.
type Config struct {
	// Namespace keys the queue topic and every coordination key.
	Namespace campaign.Namespace

	// PauseRequeueDelay is how long a message waits before being retried while
	// its campaign is paused.
	PauseRequeueDelay time.Duration

	// Precheck runs before a campaign is subscribed and can refuse.
	//
	// This is where a channel asserts a precondition that makes the whole
	// campaign unsendable rather than one entry — a template that is no longer
	// approved, a number that is no longer connected. The channel is expected to
	// stop the campaign itself before returning the error, because only it knows
	// whether the condition is permanent.
	//
	// Optional: nil means always allowed.
	Precheck func(campaignID string) error

	// Pace returns how long to wait after a successful send before taking the
	// next message off this campaign's queue.
	//
	// A function rather than a constant because the two channels differ
	// fundamentally here: the Cloud API is throttled by Meta and needs only a
	// token delay, while a linked device has to look human and derives its
	// delay from the instance's own jitter range.
	//
	// Optional: nil means no pacing.
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

// Runner owns one channel's campaign queues.
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

// Start re-attaches to every campaign that was running when the process died.
//
// A campaign whose queue is already empty is completed rather than subscribed:
// without this, a campaign that finished during a restart would sit at RUNNING
// forever, because the completion signal is a message arriving and there are no
// more messages.
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

// SubscribeToCampaign attaches a consumer to one campaign's queue.
func (r *Runner) SubscribeToCampaign(campaignID string) error {
	r.mu.RLock()
	already := r.subscribed[campaignID]
	r.mu.RUnlock()
	if already {
		return nil
	}

	// Clearing the held flags comes BEFORE the precheck, not after.
	//
	// A campaign being started is no longer stopped or paused regardless of
	// whether its channel then refuses it, and the refusal path sets the
	// campaign's own status instead. Clearing afterwards would leave a rejected
	// campaign carrying a stop flag that silently discards every message on the
	// next successful start.
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

// deliver applies one message, translating the handler's decision into the
// broker acknowledgement and the completion counter.
func (r *Runner) deliver(topic string, raw []byte, ack messaging.MessageAck) {
	var msg Message
	if err := json.Unmarshal(raw, &msg); err != nil {
		// A message we cannot decode will never decode. Requeuing it would spin
		// forever, so it is dropped without requeue and without counting: it
		// belongs to no entry we can identify.
		r.cfg.logf("campaign queue: failed to decode dispatch payload: %v", err)
		_ = ack.Nack(false)
		return
	}

	// The pause check is first and reads shared state rather than the local map,
	// because the operator who paused may have hit a different replica.
	if held, _ := r.shared.Exists(r.cfg.Namespace.PausedKey(msg.CampaignID)); held {
		r.republish(topic, raw, r.cfg.PauseRequeueDelay)
		_ = ack.Ack()
		return
	}
	// A stopped campaign discards in-flight work. The queue is deleted on stop,
	// but a message already read is not in the queue any more.
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

// PauseCampaignConsumer holds a campaign without discarding its queue.
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

	// Detaching the consumer is best-effort: the paused flag above is what
	// actually holds the campaign, and a broker that cannot stop a consumer
	// still delivers into a handler that requeues.
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

// ResumeCampaignConsumer lifts a pause.
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

// StopCampaignConsumer discards the campaign's remaining work.
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

// SetCounter records how many entries a dispatch just enqueued.
func (r *Runner) SetCounter(campaignID string, n int) {
	if n <= 0 {
		return
	}
	_ = r.shared.SetString(r.cfg.Namespace.RemainingKey(campaignID), strconv.Itoa(n), remainingCounterTTL)
}

// SeedCounterIfMissing rebuilds a completion counter from the database.
//
// Needed because the counter has a TTL and lives in a cache that can be flushed.
// Without it, a campaign resumed after the key expired would never reach zero
// and would stay RUNNING with an empty queue.
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

// countProcessed decrements the completion counter and finishes the campaign at
// zero.
//
// Completion is counted rather than derived from queue length, because a message
// currently being processed is already off the queue: an empty queue with work
// in flight would otherwise read as finished.
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

// queueIsEmpty reports whether both the main and the delay queue are drained.
//
// The delay queue matters: a campaign whose remaining work is all waiting on a
// retry has an empty main queue and is very much not finished.
func (r *Runner) queueIsEmpty(campaignID string) bool {
	topic := r.cfg.Namespace.DispatchTopic(campaignID)
	mainLen, err := r.sub.GetQueueLength(topic)
	if err != nil || mainLen > 0 {
		return false
	}
	delayLen, err := r.sub.GetQueueLength(topic + messaging.DelayQueueSuffix)
	return err == nil && delayLen == 0
}
