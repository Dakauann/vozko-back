package shortlink_usecase

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"vozko/domain/cache"
	"vozko/domain/shortlink"
)

func TestPublishClick(t *testing.T) {
	pub := &fakeQueuePub{}
	uc := NewPublishClickUseCase(pub)
	if err := uc.Execute(context.Background(), shortlink.ClickMessage{ShortLinkID: "l"}); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if pub.count() != 1 {
		t.Fatalf("published = %d", pub.count())
	}

	pubErr := &fakeQueuePub{err: errors.New("amqp")}
	if err := NewPublishClickUseCase(pubErr).Execute(context.Background(), shortlink.ClickMessage{}); err == nil {
		t.Fatal("expected publish error")
	}
}

func newConsumer(clickRepo *fakeClickRepo, linkRepo *fakeShortLinkRepo, ua fakeUA, shared cache.SharedState, pub *fakeQueuePub, sub *fakeQueueSub) *consumeClickUseCase {
	return NewConsumeClickUseCase(sub, pub, clickRepo, linkRepo, ua, fakeGeo{info: shortlink.GeoInfo{Country: "BR"}}, shared, "salt", time.Hour).(*consumeClickUseCase)
}

func validClickMessage() shortlink.ClickMessage {
	return shortlink.ClickMessage{
		ClickEventID: "evt-1",
		ShortLinkID:  "l",
		WorkspaceID:  "ws",
		Code:         "abc",
		OccurredAt:   time.Now().UTC(),
		IP:           "203.0.113.5",
		UserAgent:    "Mozilla/5.0 Chrome",
		Referer:      "https://ref.com/page",
		Attempt:      1,
	}
}

func TestConsumeStart(t *testing.T) {
	sub := &fakeQueueSub{}
	uc := newConsumer(&fakeClickRepo{}, &fakeShortLinkRepo{}, fakeUA{}, newFakeSharedState(), &fakeQueuePub{}, sub)
	if err := uc.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	if sub.topic != shortlink.ClickTopic || sub.handler == nil {
		t.Fatal("subscribe not wired")
	}

	ack := newFakeAck()
	payload, _ := json.Marshal(validClickMessage())
	sub.handler(payload, ack)
	ack.wait(t)

	subErr := &fakeQueueSub{err: errors.New("no conn")}
	if err := newConsumer(&fakeClickRepo{}, &fakeShortLinkRepo{}, fakeUA{}, newFakeSharedState(), &fakeQueuePub{}, subErr).Start(); err == nil {
		t.Fatal("expected start error")
	}
}

func TestConsumeHandle_AckError(t *testing.T) {
	uc := newConsumer(&fakeClickRepo{}, &fakeShortLinkRepo{}, fakeUA{}, newFakeSharedState(), &fakeQueuePub{}, &fakeQueueSub{})
	ack := newFakeAck()
	ack.ackErr = errors.New("ack failed")
	payload, _ := json.Marshal(validClickMessage())
	uc.handle(payload, ack)
	ack.wait(t)
	if !ack.acked {
		t.Fatal("ack should have been attempted")
	}
}

func TestConsumeRetryPublishError(t *testing.T) {
	clickRepo := &fakeClickRepo{RecordFn: func(ctx context.Context, c *shortlink.Click) (bool, error) { return false, errors.New("db") }}
	pub := &fakeQueuePub{err: errors.New("amqp down")}
	uc := newConsumer(clickRepo, &fakeShortLinkRepo{}, fakeUA{}, newFakeSharedState(), pub, &fakeQueueSub{})
	ack := newFakeAck()
	payload, _ := json.Marshal(validClickMessage())
	uc.handle(payload, ack)
	ack.wait(t)
}

func TestConsumeHandle_InvalidPayload(t *testing.T) {
	uc := newConsumer(&fakeClickRepo{}, &fakeShortLinkRepo{}, fakeUA{}, newFakeSharedState(), &fakeQueuePub{}, &fakeQueueSub{})
	ack := newFakeAck()
	uc.handle([]byte("{bad"), ack)
	if !ack.nacked || ack.requeue {
		t.Fatalf("invalid payload should nack without requeue: %+v", ack)
	}
}

func TestConsumeHandle_Success(t *testing.T) {
	var appliedUnique int64 = -1
	clickRepo := &fakeClickRepo{
		RecordFn: func(ctx context.Context, c *shortlink.Click) (bool, error) { return true, nil },
	}
	linkRepo := &fakeShortLinkRepo{ApplyClickFn: func(ctx context.Context, id string, uniqueDelta int64, occurredAt time.Time) error {
		appliedUnique = uniqueDelta
		return nil
	}}
	uc := newConsumer(clickRepo, linkRepo, fakeUA{info: shortlink.DeviceInfo{DeviceType: "desktop", Browser: "Chrome", OS: "Windows"}}, newFakeSharedState(), &fakeQueuePub{}, &fakeQueueSub{})

	ack := newFakeAck()
	payload, _ := json.Marshal(validClickMessage())
	uc.handle(payload, ack)
	ack.wait(t)
	if !ack.acked {
		t.Fatal("success should ack")
	}
	if appliedUnique != 1 {
		t.Fatalf("unique delta = %d want 1", appliedUnique)
	}
}

func TestConsumeHandle_ProcessErrorRetries(t *testing.T) {
	clickRepo := &fakeClickRepo{RecordFn: func(ctx context.Context, c *shortlink.Click) (bool, error) { return false, errors.New("db") }}
	pub := &fakeQueuePub{}
	uc := newConsumer(clickRepo, &fakeShortLinkRepo{}, fakeUA{}, newFakeSharedState(), pub, &fakeQueueSub{})

	ack := newFakeAck()
	payload, _ := json.Marshal(validClickMessage())
	uc.handle(payload, ack)
	ack.wait(t)
	if pub.count() != 1 {
		t.Fatalf("retry not published: %d", pub.count())
	}
}

func TestConsumeHandle_ProcessErrorExhausted(t *testing.T) {
	clickRepo := &fakeClickRepo{RecordFn: func(ctx context.Context, c *shortlink.Click) (bool, error) { return false, errors.New("db") }}
	pub := &fakeQueuePub{}
	uc := newConsumer(clickRepo, &fakeShortLinkRepo{}, fakeUA{}, newFakeSharedState(), pub, &fakeQueueSub{})

	msg := validClickMessage()
	msg.Attempt = shortlink.MaxClickProcessingAttempts
	payload, _ := json.Marshal(msg)
	ack := newFakeAck()
	uc.handle(payload, ack)
	ack.wait(t)
	if pub.count() != 0 {
		t.Fatal("exhausted attempts must not retry")
	}
}

func TestConsumeHandle_Panic(t *testing.T) {
	uc := newConsumer(&fakeClickRepo{}, &fakeShortLinkRepo{}, fakeUA{panicOn: true}, newFakeSharedState(), &fakeQueuePub{}, &fakeQueueSub{})
	ack := newFakeAck()
	payload, _ := json.Marshal(validClickMessage())
	uc.handle(payload, ack)
	ack.wait(t)
	if !ack.nacked || !ack.requeue {
		t.Fatalf("panic on attempt 1 should nack+requeue: %+v", ack)
	}
}

func TestProcess_Branches(t *testing.T) {
	t.Run("duplicate delivery", func(t *testing.T) {
		clickRepo := &fakeClickRepo{RecordFn: func(ctx context.Context, c *shortlink.Click) (bool, error) { return false, nil }}
		uc := newConsumer(clickRepo, &fakeShortLinkRepo{}, fakeUA{}, newFakeSharedState(), &fakeQueuePub{}, &fakeQueueSub{})
		if err := uc.process(validClickMessage()); err != nil {
			t.Fatalf("duplicate should be nil: %v", err)
		}
	})

	t.Run("record error", func(t *testing.T) {
		clickRepo := &fakeClickRepo{RecordFn: func(ctx context.Context, c *shortlink.Click) (bool, error) { return false, errors.New("db") }}
		uc := newConsumer(clickRepo, &fakeShortLinkRepo{}, fakeUA{}, newFakeSharedState(), &fakeQueuePub{}, &fakeQueueSub{})
		if err := uc.process(validClickMessage()); err == nil {
			t.Fatal("expected record error")
		}
	})

	t.Run("daily stats error", func(t *testing.T) {
		clickRepo := &fakeClickRepo{DailyFn: func(ctx context.Context, deltas []shortlink.DailyStatDelta) error { return errors.New("db") }}
		uc := newConsumer(clickRepo, &fakeShortLinkRepo{}, fakeUA{}, newFakeSharedState(), &fakeQueuePub{}, &fakeQueueSub{})
		if err := uc.process(validClickMessage()); err == nil {
			t.Fatal("expected daily stats error")
		}
	})

	t.Run("apply click error", func(t *testing.T) {
		linkRepo := &fakeShortLinkRepo{ApplyClickFn: func(ctx context.Context, id string, uniqueDelta int64, occurredAt time.Time) error { return errors.New("db") }}
		uc := newConsumer(&fakeClickRepo{}, linkRepo, fakeUA{}, newFakeSharedState(), &fakeQueuePub{}, &fakeQueueSub{})
		if err := uc.process(validClickMessage()); err == nil {
			t.Fatal("expected apply click error")
		}
	})

	t.Run("empty ip no unique", func(t *testing.T) {
		var applied int64 = -1
		linkRepo := &fakeShortLinkRepo{ApplyClickFn: func(ctx context.Context, id string, uniqueDelta int64, occurredAt time.Time) error {
			applied = uniqueDelta
			return nil
		}}
		uc := newConsumer(&fakeClickRepo{}, linkRepo, fakeUA{}, newFakeSharedState(), &fakeQueuePub{}, &fakeQueueSub{})
		msg := validClickMessage()
		msg.IP = ""
		if err := uc.process(msg); err != nil {
			t.Fatalf("process: %v", err)
		}
		if applied != 0 {
			t.Fatalf("empty ip unique = %d want 0", applied)
		}
	})

	t.Run("repeat visitor no unique", func(t *testing.T) {
		shared := newFakeSharedState()
		var deltas []int64
		linkRepo := &fakeShortLinkRepo{ApplyClickFn: func(ctx context.Context, id string, uniqueDelta int64, occurredAt time.Time) error {
			deltas = append(deltas, uniqueDelta)
			return nil
		}}
		uc := newConsumer(&fakeClickRepo{}, linkRepo, fakeUA{}, shared, &fakeQueuePub{}, &fakeQueueSub{})
		_ = uc.process(validClickMessage())
		_ = uc.process(validClickMessage())
		if len(deltas) != 2 || deltas[0] != 1 || deltas[1] != 0 {
			t.Fatalf("unique deltas = %v want [1 0]", deltas)
		}
	})

	t.Run("setnx error no unique", func(t *testing.T) {
		shared := newFakeSharedState()
		shared.setNXErr = errors.New("redis")
		var applied int64 = -1
		linkRepo := &fakeShortLinkRepo{ApplyClickFn: func(ctx context.Context, id string, uniqueDelta int64, occurredAt time.Time) error {
			applied = uniqueDelta
			return nil
		}}
		uc := newConsumer(&fakeClickRepo{}, linkRepo, fakeUA{}, shared, &fakeQueuePub{}, &fakeQueueSub{})
		_ = uc.process(validClickMessage())
		if applied != 0 {
			t.Fatalf("setnx error unique = %d want 0", applied)
		}
	})

	t.Run("nil shared no unique", func(t *testing.T) {
		var applied int64 = -1
		linkRepo := &fakeShortLinkRepo{ApplyClickFn: func(ctx context.Context, id string, uniqueDelta int64, occurredAt time.Time) error {
			applied = uniqueDelta
			return nil
		}}
		uc := newConsumer(&fakeClickRepo{}, linkRepo, fakeUA{}, nil, &fakeQueuePub{}, &fakeQueueSub{})
		_ = uc.process(validClickMessage())
		if applied != 0 {
			t.Fatalf("nil shared unique = %d want 0", applied)
		}
	})
}

func TestBuildDailyStatDeltas(t *testing.T) {
	click := &shortlink.Click{
		ShortLinkID: "l", WorkspaceID: "ws",
		OccurredAt: time.Date(2026, 7, 22, 15, 0, 0, 0, time.UTC),
		Country:    "BR", DeviceType: "mobile", RefererDomain: "ref.com", Browser: "Chrome", OS: "Android",
	}
	deltas := buildDailyStatDeltas(click, 1)
	if len(deltas) != 6 {
		t.Fatalf("expected 6 deltas, got %d", len(deltas))
	}
	if deltas[0].Dimension != shortlink.DimTotal || deltas[0].UniqueClicks != 1 {
		t.Fatalf("total delta wrong: %+v", deltas[0])
	}

	empty := &shortlink.Click{ShortLinkID: "l", OccurredAt: click.OccurredAt}
	if got := buildDailyStatDeltas(empty, 0); len(got) != 1 {
		t.Fatalf("empty dims should yield only total, got %d", len(got))
	}
}
