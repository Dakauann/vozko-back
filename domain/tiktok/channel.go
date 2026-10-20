package tiktok

import "vozko/domain/webhook"

func TopicFor(event string) (string, bool) {
	switch event {
	case eventCommentUpdate:
		return webhook.TopicTikTokComment, true
	case eventReceive, eventReceiveEU, eventSend, eventReferral, eventMarkRead, eventAutoConfig, eventAutoAudit, eventHighIntent:
		return webhook.TopicTikTokMessage, true
	}
	return "", false
}
