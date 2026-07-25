package workflow

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func drain(ch <-chan RuntimeEvent, want int, timeout time.Duration) ([]RuntimeEvent, error) {
	out := make([]RuntimeEvent, 0, want)
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	for len(out) < want {
		select {
		case ev, ok := <-ch:
			if !ok {
				return out, errors.New("channel closed before receiving expected events")
			}
			out = append(out, ev)
		case <-deadline.C:
			return out, errors.New("timeout waiting for events")
		}
	}
	return out, nil
}

func TestEventBus_PublishFanout(t *testing.T) {
	bus := NewInMemoryEventBus()
	bus.Publish(RuntimeEvent{Kind: EventFirstFrameSent})

	chA, cancelA := bus.Subscribe()
	defer cancelA()
	chB, cancelB := bus.Subscribe()
	defer cancelB()

	bus.Publish(RuntimeEvent{Kind: EventSpeechStart})

	for _, ch := range []<-chan RuntimeEvent{chA, chB} {
		select {
		case ev := <-ch:
			if ev.Kind != EventSpeechStart {
				t.Fatalf("expected speech_start, got %s", ev.Kind)
			}
			if ev.At.IsZero() {
				t.Fatalf("expected At to be populated")
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber did not receive event")
		}
	}
}

func TestEventBus_FilterKinds(t *testing.T) {
	bus := NewInMemoryEventBus()
	bus.Publish(RuntimeEvent{Kind: EventFirstFrameSent})

	speechCh, cancel := bus.Subscribe(EventSpeechStart)
	defer cancel()

	bus.Publish(RuntimeEvent{Kind: EventDTMFDigit, Payload: '5'})
	bus.Publish(RuntimeEvent{Kind: EventSpeechStart})

	select {
	case ev := <-speechCh:
		if ev.Kind != EventSpeechStart {
			t.Fatalf("filter leaked: got %s", ev.Kind)
		}
	case <-time.After(time.Second):
		t.Fatal("filtered subscriber did not receive matching event")
	}

	select {
	case ev := <-speechCh:
		t.Fatalf("filter leaked extra event: %s", ev.Kind)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestEventBus_FirstFrameGate(t *testing.T) {
	bus := NewInMemoryEventBus()
	ch, cancel := bus.Subscribe(EventSpeechStart)
	defer cancel()

	bus.Publish(RuntimeEvent{Kind: EventSpeechStart})

	select {
	case ev := <-ch:
		t.Fatalf("first-frame gate failed: received %s", ev.Kind)
	case <-time.After(50 * time.Millisecond):
	}

	bus.Publish(RuntimeEvent{Kind: EventFirstFrameSent})
	bus.Publish(RuntimeEvent{Kind: EventSpeechStart})

	select {
	case ev := <-ch:
		if ev.Kind != EventSpeechStart {
			t.Fatalf("expected speech_start, got %s", ev.Kind)
		}
	case <-time.After(time.Second):
		t.Fatal("gate did not release speech_start after first frame")
	}
}

func TestEventBus_UnsubscribeCloses(t *testing.T) {
	bus := NewInMemoryEventBus()
	ch, cancel := bus.Subscribe()
	cancel()
	cancel()

	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected channel to be closed after unsubscribe")
		}
	case <-time.After(time.Second):
		t.Fatal("channel not closed after unsubscribe")
	}
}

func TestEventBus_InterruptContextCancelsOnMatch(t *testing.T) {
	bus := NewInMemoryEventBus()
	bus.Publish(RuntimeEvent{Kind: EventFirstFrameSent})

	parent, parentCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer parentCancel()

	ictx, release := bus.InterruptContext(parent, EventSpeechStart, EventDTMFDigit)
	defer release()

	if ictx.Err() != nil {
		t.Fatal("interrupt ctx already cancelled")
	}

	go func() {
		time.Sleep(20 * time.Millisecond)
		bus.Publish(RuntimeEvent{Kind: EventSpeechStart})
	}()

	select {
	case <-ictx.Done():
	case <-time.After(time.Second):
		t.Fatal("interrupt ctx was not cancelled by matching event")
	}
	if !errors.Is(ictx.Err(), context.Canceled) {
		t.Fatalf("expected Canceled, got %v", ictx.Err())
	}
}

func TestEventBus_InterruptContextIgnoresNonMatching(t *testing.T) {
	bus := NewInMemoryEventBus()
	bus.Publish(RuntimeEvent{Kind: EventFirstFrameSent})

	ictx, release := bus.InterruptContext(context.Background(), EventDTMFDigit)
	defer release()

	bus.Publish(RuntimeEvent{Kind: EventSpeechStart})
	bus.Publish(RuntimeEvent{Kind: EventHangup})

	select {
	case <-ictx.Done():
		t.Fatalf("interrupt ctx cancelled by non-matching event: %v", ictx.Err())
	case <-time.After(80 * time.Millisecond):
	}
}

func TestEventBus_InterruptContextParentCancellationCascades(t *testing.T) {
	bus := NewInMemoryEventBus()
	parent, parentCancel := context.WithCancel(context.Background())
	ictx, release := bus.InterruptContext(parent, EventSpeechStart)
	defer release()

	parentCancel()

	select {
	case <-ictx.Done():
	case <-time.After(time.Second):
		t.Fatal("interrupt ctx did not cascade parent cancellation")
	}
}

func TestEventBus_PublishNonBlockingOnSlowSubscriber(t *testing.T) {
	bus := NewInMemoryEventBus()
	bus.Publish(RuntimeEvent{Kind: EventFirstFrameSent})

	_, cancel := bus.Subscribe(EventSpeechStart)
	defer cancel()

	done := make(chan struct{})
	go func() {
		for i := 0; i < eventSubscriberBuffer*4; i++ {
			bus.Publish(RuntimeEvent{Kind: EventSpeechStart})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("publisher blocked on slow subscriber")
	}
}

func TestEventBus_ConcurrentPublishSubscribe(t *testing.T) {
	bus := NewInMemoryEventBus()
	bus.Publish(RuntimeEvent{Kind: EventFirstFrameSent})

	var wg sync.WaitGroup
	var received atomic.Int64

	for i := 0; i < 4; i++ {
		ch, cancel := bus.Subscribe(EventSpeechStart)
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer cancel()
			deadline := time.After(500 * time.Millisecond)
			for {
				select {
				case _, ok := <-ch:
					if !ok {
						return
					}
					received.Add(1)
				case <-deadline:
					return
				}
			}
		}()
	}

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				bus.Publish(RuntimeEvent{Kind: EventSpeechStart})
			}
		}()
	}
	wg.Wait()
	if received.Load() == 0 {
		t.Fatal("no events received under concurrency")
	}
}
