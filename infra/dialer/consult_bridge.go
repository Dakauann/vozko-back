package dialer

import "sync"

type ConsultBridge struct {
	a, b      *ConsultEndpoint
	closeOnce sync.Once
}

type ConsultEndpoint struct {
	bridge  *ConsultBridge
	inbound chan []byte
	peer    *ConsultEndpoint
	mu      sync.Mutex
	closed  bool
}

func NewConsultBridge(bufferFrames int) *ConsultBridge {
	if bufferFrames <= 0 {
		bufferFrames = 10
	}
	b := &ConsultBridge{}
	b.a = &ConsultEndpoint{bridge: b, inbound: make(chan []byte, bufferFrames)}
	b.b = &ConsultEndpoint{bridge: b, inbound: make(chan []byte, bufferFrames)}
	b.a.peer = b.b
	b.b.peer = b.a
	return b
}

func (b *ConsultBridge) A() *ConsultEndpoint { return b.a }

func (b *ConsultBridge) B() *ConsultEndpoint { return b.b }

func (b *ConsultBridge) Close() {
	b.closeOnce.Do(func() {
		b.a.markClosed()
		b.b.markClosed()
	})
}

func (e *ConsultEndpoint) Send(pcm []byte) {
	if e == nil || e.peer == nil {
		return
	}
	e.peer.mu.Lock()
	if e.peer.closed {
		e.peer.mu.Unlock()
		return
	}
	select {
	case e.peer.inbound <- pcm:
	default:

	}
	e.peer.mu.Unlock()
}

func (e *ConsultEndpoint) Recv() <-chan []byte {
	if e == nil {
		return nil
	}
	return e.inbound
}

func (e *ConsultEndpoint) markClosed() {
	e.mu.Lock()
	if !e.closed {
		e.closed = true
		close(e.inbound)
	}
	e.mu.Unlock()
}
