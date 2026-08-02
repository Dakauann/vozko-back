package telegram

import (
	"context"
	"log"
	"strconv"
	"time"

	"vozko/domain/cache"
	tgdomain "vozko/domain/telegram"
)

// Telegram's published limits, from the Bot FAQ:
//
//	"In a single chat, avoid sending more than one message per second. We may
//	 allow short bursts that go over this limit, but eventually you'll begin
//	 receiving 429 errors."
//	"For bulk notifications, bots are not able to broadcast more than about 30
//	 messages per second"
//
// Both are enforced, because they bind independently: one busy conversation must
// not consume the whole bot's budget, and many quiet conversations together
// still can.
const (
	perChatPerSecond = tgdomain.PerChatMessagesPerSecond
	perBotPerSecond  = tgdomain.PerBotMessagesPerSecond
)

// throttled wraps a BotAPI with the two send budgets.
//
// Only the sending methods are throttled. Reads (getMe, getWebhookInfo, getFile)
// and presence signals are not subject to the broadcast limit, and throttling
// them would slow the health cron and media ingest for no reason.
type throttled struct {
	tgdomain.BotAPI

	perChat cache.RateLimiter
	perBot  cache.RateLimiter
}

// NewThrottled decorates a client with per-chat and per-bot rate limiting.
//
// A nil factory returns the client unchanged, so a deployment without Redis
// still works — it simply relies on Telegram's own 429 plus the retry_after the
// caller already honours.
func NewThrottled(api tgdomain.BotAPI, factory cache.RateLimiterFactory) tgdomain.BotAPI {
	if api == nil || factory == nil {
		return api
	}
	return &throttled{
		BotAPI:  api,
		perChat: factory("tg_send_chat", perChatPerSecond, time.Second),
		perBot:  factory("tg_send_bot", perBotPerSecond, time.Second),
	}
}

// acquire waits for both budgets.
//
// It waits rather than failing: a send is a user-visible action already
// committed to by an operator, and the alternative — surfacing "rate limited" in
// the composer — is worse than a sub-second delay. The context bounds the wait,
// so a genuinely saturated bot still fails fast rather than hanging.
func (t *throttled) acquire(ctx context.Context, botKey string, chatID int64) error {
	chatKey := botKey + ":" + strconv.FormatInt(chatID, 10)
	for _, attempt := range []struct {
		limiter cache.RateLimiter
		key     string
	}{
		{t.perChat, chatKey},
		{t.perBot, botKey},
	} {
		if attempt.limiter == nil {
			continue
		}
		if err := waitFor(ctx, attempt.limiter, attempt.key); err != nil {
			return err
		}
	}
	return nil
}

func waitFor(ctx context.Context, limiter cache.RateLimiter, key string) error {
	// Bounded retries: the windows are one second, so a handful of waits covers
	// any legitimate contention. Looping forever would turn a saturated bot into
	// a stuck request.
	const maxAttempts = 5

	for i := 0; i < maxAttempts; i++ {
		allowed, retryAfter, err := limiter.Allow(key)
		if err != nil {
			// Fail OPEN on a limiter outage. Telegram answers an over-limit send
			// with an explicit retry_after that the caller honours, so the
			// provider remains the backstop; refusing to send because Redis is
			// down would be a self-inflicted outage.
			log.Printf("[telegram] rate limiter unavailable for %s, proceeding: %v", key, err)
			return nil
		}
		if allowed {
			return nil
		}
		if retryAfter <= 0 || retryAfter > time.Second {
			retryAfter = 200 * time.Millisecond
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(retryAfter):
		}
	}
	// Out of attempts: proceed and let Telegram's own 429 (with its exact
	// retry_after) drive the caller's backoff.
	return nil
}

// botKeyFor derives a stable limiter key from a token without ever putting the
// token itself in Redis.
//
// A Bot API token is "<bot_id>:<secret>", so the numeric prefix identifies the
// bot uniquely and leaks nothing.
func botKeyFor(token string) string {
	for i := 0; i < len(token); i++ {
		if token[i] == ':' {
			return token[:i]
		}
	}
	return "unknown"
}

func (t *throttled) SendText(ctx context.Context, token string, in tgdomain.SendTextInput) (*tgdomain.SendResult, error) {
	if err := t.acquire(ctx, botKeyFor(token), in.ChatID); err != nil {
		return nil, err
	}
	return t.BotAPI.SendText(ctx, token, in)
}

func (t *throttled) SendMedia(ctx context.Context, token string, in tgdomain.SendMediaInput) (*tgdomain.SendResult, error) {
	if err := t.acquire(ctx, botKeyFor(token), in.ChatID); err != nil {
		return nil, err
	}
	return t.BotAPI.SendMedia(ctx, token, in)
}
