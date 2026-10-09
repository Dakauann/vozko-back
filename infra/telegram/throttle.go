package telegram

import (
	"context"
	"errors"
	"log"
	"strconv"
	"time"

	"vozko/domain/cache"
	tgdomain "vozko/domain/telegram"
)

const (
	perChatPerSecond = tgdomain.PerChatMessagesPerSecond
	perBotPerSecond  = tgdomain.PerBotMessagesPerSecond
)

type throttled struct {
	tgdomain.BotAPI

	perChat cache.RateLimiter
	perBot  cache.RateLimiter
}

var errThrottleDependencies = errors.New("telegram: throttle requires a bot api and a rate limiter factory")

func NewThrottled(api tgdomain.BotAPI, factory cache.RateLimiterFactory) (tgdomain.BotAPI, error) {
	if api == nil || factory == nil {
		return nil, errThrottleDependencies
	}
	return &throttled{
		BotAPI:  api,
		perChat: factory("tg_send_chat", perChatPerSecond, time.Second),
		perBot:  factory("tg_send_bot", perBotPerSecond, time.Second),
	}, nil
}

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
			return localRateLimited("rate limiter missing")
		}
		if err := waitFor(ctx, attempt.limiter, attempt.key); err != nil {
			return err
		}
	}
	return nil
}

func waitFor(ctx context.Context, limiter cache.RateLimiter, key string) error {
	const maxAttempts = 5

	for i := 0; i < maxAttempts; i++ {
		allowed, retryAfter, err := limiter.Allow(key)
		if err != nil {
			log.Printf("[telegram] rate limiter unavailable for %s, refusing the send: %v", key, err)
			return localRateLimited("rate limiter unavailable")
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
	return localRateLimited("local rate limit reached")
}

func localRateLimited(reason string) error {
	return &tgdomain.APIError{Code: 429, Description: reason, RetryAfter: 1}
}

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
