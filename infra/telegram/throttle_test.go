package telegram

import (
	"context"
	"errors"
	"testing"
	"time"

	"vozko/domain/cache"
	tgdomain "vozko/domain/telegram"
)

type scriptedLimiter struct {
	allowed bool
	err     error
	calls   int
}

func (s *scriptedLimiter) Allow(string) (bool, time.Duration, error) {
	s.calls++
	return s.allowed, time.Millisecond, s.err
}

type countingBot struct {
	tgdomain.BotAPI
	sent int
}

func (c *countingBot) SendText(context.Context, string, tgdomain.SendTextInput) (*tgdomain.SendResult, error) {
	c.sent++
	return &tgdomain.SendResult{}, nil
}

func throttledWith(bot tgdomain.BotAPI, limiter cache.RateLimiter) *throttled {
	return &throttled{BotAPI: bot, perChat: limiter, perBot: limiter}
}

func TestThrottled_RefusesInsteadOfSendingWhenTheLimiterCannotAnswer(t *testing.T) {
	cases := []struct {
		name    string
		limiter *scriptedLimiter
	}{
		{"limiter error", &scriptedLimiter{err: errors.New("redis down")}},
		{"still limited after every attempt", &scriptedLimiter{allowed: false}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bot := &countingBot{}
			_, err := throttledWith(bot, tc.limiter).SendText(context.Background(), "123:abc", tgdomain.SendTextInput{ChatID: 9})

			var apiErr *tgdomain.APIError
			if !errors.As(err, &apiErr) || !apiErr.Retryable() {
				t.Fatalf("want a retryable refusal, got %v", err)
			}
			if bot.sent != 0 {
				t.Fatal("nothing may be sent without the limiter's permission")
			}
		})
	}
}

func TestThrottled_SendsWhenAllowed(t *testing.T) {
	bot := &countingBot{}
	limiter := &scriptedLimiter{allowed: true}
	if _, err := throttledWith(bot, limiter).SendText(context.Background(), "123:abc", tgdomain.SendTextInput{ChatID: 9}); err != nil {
		t.Fatal(err)
	}
	if bot.sent != 1 || limiter.calls != 2 {
		t.Fatalf("sent %d after %d limiter calls", bot.sent, limiter.calls)
	}
}

func TestNewThrottled_RefusesAMissingDependency(t *testing.T) {
	factory := func(string, int, time.Duration) cache.RateLimiter { return &scriptedLimiter{allowed: true} }
	cases := []struct {
		name    string
		api     tgdomain.BotAPI
		factory cache.RateLimiterFactory
	}{
		{"no limiter factory", &countingBot{}, nil},
		{"no bot api", nil, factory},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if api, err := NewThrottled(tc.api, tc.factory); err == nil || api != nil {
				t.Fatalf("want a refusal, got %v, %v", api, err)
			}
		})
	}
	if api, err := NewThrottled(&countingBot{}, factory); err != nil || api == nil {
		t.Fatalf("want a throttled api, got %v, %v", api, err)
	}
}

func TestThrottled_AMissingLimiterRefusesTheSend(t *testing.T) {
	bot := &countingBot{}
	_, err := (&throttled{BotAPI: bot, perBot: &scriptedLimiter{allowed: true}}).SendText(context.Background(), "123:abc", tgdomain.SendTextInput{ChatID: 9})

	var apiErr *tgdomain.APIError
	if !errors.As(err, &apiErr) || bot.sent != 0 {
		t.Fatalf("want a refusal with nothing sent, got %v and %d sends", err, bot.sent)
	}
}
