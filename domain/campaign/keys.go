package campaign

import "fmt"

// The queue topic and the coordination keys a campaign runner needs.
//
// Built here, from a channel-supplied namespace, so two channels' campaigns can
// never collide on a Redis key or a queue name. Spelling them inline at each
// call site is how one of eleven sites ends up reading a key nobody writes —
// which fails silently, as a campaign that never pauses.

// Namespace identifies one channel's campaign machinery.
//
// Topic is the RabbitMQ topic prefix and Key is the Redis key prefix. They are
// separate values because the queue name is part of an operational surface
// (queues are listed, drained and inspected by hand) while the Redis prefix is
// not, and forcing them to be the same string would make one of the two ugly.
type Namespace struct {
	Topic string
	Key   string
}

// DispatchTopic is the per-campaign queue.
//
// One queue per campaign, not one queue per channel: stopping a campaign deletes
// its queue, which is the only way to discard queued work without draining and
// re-publishing everything else.
func (n Namespace) DispatchTopic(campaignID string) string {
	return fmt.Sprintf("%s.%s", n.Topic, campaignID)
}

// PausedKey is set while a campaign is paused. Consumers check it per message
// and requeue rather than send.
func (n Namespace) PausedKey(campaignID string) string {
	return fmt.Sprintf("%s:paused:%s", n.Key, campaignID)
}

// StoppedKey is set when a campaign is stopped, so an in-flight message that
// was read before the queue was deleted does not get sent.
func (n Namespace) StoppedKey(campaignID string) string {
	return fmt.Sprintf("%s:stopped:%s", n.Key, campaignID)
}

// RemainingKey counts entries still to process.
//
// The completion signal. Deriving completion from queue length instead is
// unreliable: a message being processed is already off the queue, so an empty
// queue with work in flight looks finished.
func (n Namespace) RemainingKey(campaignID string) string {
	return fmt.Sprintf("%s:remaining:%s", n.Key, campaignID)
}

// QuickSendLockKey serialises quick-send bursts on one campaign.
func (n Namespace) QuickSendLockKey(campaignID string) string {
	return fmt.Sprintf("%s:quicksend_lock:%s", n.Key, campaignID)
}
