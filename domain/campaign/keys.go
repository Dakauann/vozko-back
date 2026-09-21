package campaign

import "fmt"

type Namespace struct {
	Topic string
	Key   string
}

func (n Namespace) DispatchTopic(campaignID string) string {
	return fmt.Sprintf("%s.%s", n.Topic, campaignID)
}

func (n Namespace) PausedKey(campaignID string) string {
	return fmt.Sprintf("%s:paused:%s", n.Key, campaignID)
}

func (n Namespace) StoppedKey(campaignID string) string {
	return fmt.Sprintf("%s:stopped:%s", n.Key, campaignID)
}

func (n Namespace) RemainingKey(campaignID string) string {
	return fmt.Sprintf("%s:remaining:%s", n.Key, campaignID)
}

func (n Namespace) QuickSendLockKey(campaignID string) string {
	return fmt.Sprintf("%s:quicksend_lock:%s", n.Key, campaignID)
}
