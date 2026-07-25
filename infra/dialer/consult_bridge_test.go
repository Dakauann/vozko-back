package dialer

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestConsultBridge_BidirectionalDelivery(t *testing.T) {
	t.Parallel()
	b := NewConsultBridge(4)

	b.A().Send([]byte{0x01, 0x02})
	b.B().Send([]byte{0x03, 0x04})

	select {
	case got := <-b.B().Recv():
		if len(got) != 2 || got[0] != 0x01 || got[1] != 0x02 {
			t.Fatalf("B received %v, want [1 2]", got)
		}
	case <-time.After(time.Second):
		t.Fatalf("B did not receive frame from A")
	}

	select {
	case got := <-b.A().Recv():
		if len(got) != 2 || got[0] != 0x03 || got[1] != 0x04 {
			t.Fatalf("A received %v, want [3 4]", got)
		}
	case <-time.After(time.Second):
		t.Fatalf("A did not receive frame from B")
	}
}

func TestConsultBridge_DropsOnFullBuffer(t *testing.T) {
	t.Parallel()
	b := NewConsultBridge(2)

	b.A().Send([]byte{1})
	b.A().Send([]byte{2})
	done := make(chan struct{})
	go func() {
		b.A().Send([]byte{3})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("Send blocked on full buffer; expected drop")
	}
}

func TestConsultBridge_CloseIsIdempotent(t *testing.T) {
	t.Parallel()
	b := NewConsultBridge(2)
	b.Close()
	b.Close()

	b.A().Send([]byte{1})

	if _, ok := <-b.A().Recv(); ok {
		t.Fatalf("expected closed recv channel")
	}
}

type countingSink struct{ frames atomic.Int32 }

func (c *countingSink) SendAudio(pcm16 []byte) error {
	if len(pcm16) != sipBytesPerFrame {
		return nil
	}
	c.frames.Add(1)
	return nil
}

func TestSilencePlayer_PumpsAndStops(t *testing.T) {
	t.Parallel()
	sink := &countingSink{}
	p := NewSilencePlayer(sink)
	p.Start(context.Background())

	time.Sleep(120 * time.Millisecond)
	p.Stop()
	got := sink.frames.Load()
	if got < 3 {
		t.Fatalf("expected at least 3 silence frames, got %d", got)
	}

	freeze := sink.frames.Load()
	time.Sleep(60 * time.Millisecond)
	if sink.frames.Load() != freeze {
		t.Fatalf("SilencePlayer kept pumping after Stop: %d -> %d", freeze, sink.frames.Load())
	}
}

func TestSilencePlayer_StopWithoutStart(t *testing.T) {
	t.Parallel()
	p := NewSilencePlayer(&countingSink{})

	done := make(chan struct{})
	go func() {
		p.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("Stop without Start blocked")
	}
}
