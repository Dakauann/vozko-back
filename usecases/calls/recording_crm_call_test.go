package calls_usecase

import (
	"bytes"
	"sync"
	"testing"
	"time"

	"vozko/domain/conversation"
)

type fakeCRMCall struct {
	id      string
	audioIn chan []byte
	events  chan conversation.CallEvent
	done    chan struct{}

	mu   sync.Mutex
	sent [][]byte
}

func newFakeCRMCall(id string) *fakeCRMCall {
	return &fakeCRMCall{
		id:      id,
		audioIn: make(chan []byte, 64),
		events:  make(chan conversation.CallEvent, 8),
		done:    make(chan struct{}),
	}
}

func (f *fakeCRMCall) ID() string { return f.id }
func (f *fakeCRMCall) SendAudio(pcm16 []byte) error {
	f.mu.Lock()
	f.sent = append(f.sent, append([]byte(nil), pcm16...))
	f.mu.Unlock()
	return nil
}
func (f *fakeCRMCall) AudioStream() <-chan []byte            { return f.audioIn }
func (f *fakeCRMCall) Events() <-chan conversation.CallEvent { return f.events }
func (f *fakeCRMCall) Done() <-chan struct{}                 { return f.done }
func (f *fakeCRMCall) Hangup() error                         { return nil }

type capturingStorage struct {
	mu       sync.Mutex
	key      string
	data     []byte
	uploaded chan struct{}
}

func newCapturingStorage() *capturingStorage {
	return &capturingStorage{uploaded: make(chan struct{}, 1)}
}

func (s *capturingStorage) UploadFile(key string, data []byte, _ string) error {
	s.mu.Lock()
	s.key = key
	s.data = append([]byte(nil), data...)
	s.mu.Unlock()
	select {
	case s.uploaded <- struct{}{}:
	default:
	}
	return nil
}
func (s *capturingStorage) GetFileURL(key string) string { return "https://r2.example/" + key }

type noopPub struct{}

func (noopPub) Publish(topic string, message []byte) error { return nil }
func (noopPub) PublishWithDelay(topic string, message []byte, delay time.Duration) error {
	return nil
}
func (noopPub) ValidateConnection() error { return nil }

func silentFrame() []byte { return make([]byte, 320) } // 160 samples PCM16 = 20ms @ 8kHz

func toneFrame(v int16) []byte {
	b := make([]byte, 320)
	for i := 0; i < len(b); i += 2 {
		b[i] = byte(v)
		b[i+1] = byte(v >> 8)
	}
	return b
}

func TestRecordingCRMCall_RecordsBothDirectionsAndUploads(t *testing.T) {
	storage := newCapturingStorage()
	pool := NewRecordingUploadPool(noopPub{}, storage, t.TempDir(), 2, nil)
	defer pool.Shutdown()

	inner := newFakeCRMCall("wa-call-123")
	rec := NewRecordingCRMCall(inner, pool, "ws-1", "entry-1", "lead-1")
	if rec == nil {
		t.Fatal("expected decorator, got nil")
	}
	if rec.ID() != "wa-call-123" {
		t.Fatalf("ID passthrough wrong: %s", rec.ID())
	}

	out := rec.AudioStream()

	// Remote (caller) audio in, agent (bot) audio out, a few hundred ms each.
	go func() {
		for i := 0; i < 25; i++ {
			inner.audioIn <- toneFrame(1000)
			_ = rec.SendAudio(toneFrame(-1000))
			time.Sleep(time.Millisecond)
		}
		close(inner.audioIn)
		close(inner.done)
	}()

	forwarded := 0
	timeout := time.After(5 * time.Second)
drain:
	for {
		select {
		case _, ok := <-out:
			if !ok {
				break drain
			}
			forwarded++
		case <-timeout:
			t.Fatal("timeout draining forwarded audio")
		}
	}
	if forwarded == 0 {
		t.Fatal("expected caller audio to be forwarded to the consumer")
	}

	if len(inner.sent) == 0 {
		t.Fatal("expected bot audio to reach the inner call")
	}

	select {
	case <-storage.uploaded:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for recording upload")
	}

	storage.mu.Lock()
	key, data := storage.key, storage.data
	storage.mu.Unlock()

	if !bytes.HasPrefix([]byte(key), []byte("recordings/wa-call-123_")) || !bytes.HasSuffix([]byte(key), []byte(".wav")) {
		t.Fatalf("unexpected recording key: %s", key)
	}
	if len(data) <= 44 {
		t.Fatalf("expected non-empty WAV payload, got %d bytes", len(data))
	}
	if !bytes.HasPrefix(data, []byte("RIFF")) || !bytes.Contains(data[:16], []byte("WAVE")) {
		t.Fatal("uploaded payload is not a WAV file")
	}
}

func TestRecordingCRMCall_NilWhenNoPool(t *testing.T) {
	inner := newFakeCRMCall("wa-call-x")
	if rec := NewRecordingCRMCall(inner, nil, "ws", "", ""); rec != nil {
		t.Fatal("expected nil decorator when pool is nil")
	}
}
