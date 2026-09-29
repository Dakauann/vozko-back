package voipinfra

import "time"

type expiryReason string

const (
	expiryNone         expiryReason = ""
	expiryMediaTimeout expiryReason = "no RTP received within the media timeout"
	expiryMaxDuration  expiryReason = "maximum call duration reached"
)

type callLimits struct {
	MediaTimeout  time.Duration
	MaxDuration   time.Duration
	CheckInterval time.Duration
}

func (l callLimits) expiry(now, answeredAt, lastPacket time.Time) expiryReason {
	if now.Sub(answeredAt) >= l.MaxDuration {
		return expiryMaxDuration
	}
	lastActivity := answeredAt
	if lastPacket.After(lastActivity) {
		lastActivity = lastPacket
	}
	if now.Sub(lastActivity) > l.MediaTimeout {
		return expiryMediaTimeout
	}
	return expiryNone
}
