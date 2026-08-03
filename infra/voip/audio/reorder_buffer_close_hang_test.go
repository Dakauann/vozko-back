package audio

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"vozko/domain/voip"
)

// wedgedMedia simulates a half-open / wedged RTP socket: ReadRTP blocks forever and
// neither Close() nor UnblockReaders() wakes it. This is exactly the production
// condition that deadlocked RTPReorderBuffer.Close(), pprof showed the bridge
// goroutine stuck at `<-rb.doneCh` in Close() for ~19h because mainLoop never
// exited (it watched a context Close() didn't cancel) and inner.Close() never
// unblocked the read. Because the slot is released by `defer releaseCallSlot` only
// after Close() returns, that hang leaked the workspace call slot permanently.
type wedgedMedia struct {
	block        chan struct{} // never closed by the socket itself → ReadRTP blocks
	unblockCalls int32
	closeCalls   int32
}

func newWedgedMedia() *wedgedMedia { return &wedgedMedia{block: make(chan struct{})} }

func (m *wedgedMedia) ReadRTP(buf []byte, packet interface{}) (int, error) {
	<-m.block // wedged: the read never returns on its own; Close/deadline are ignored
	return 0, nil
}
func (m *wedgedMedia) WriteRTP(packet interface{}) error { return nil }
func (m *wedgedMedia) LocalAddr() net.Addr               { return &net.UDPAddr{Port: 10000} }
func (m *wedgedMedia) RemoteAddr() net.Addr              { return &net.UDPAddr{Port: 20000} }

// Close and UnblockReaders record the call but deliberately do NOT unblock the
// read, the whole point is that the socket is wedged, so teardown must not depend
// on the read ever returning.
func (m *wedgedMedia) Close() error {
	atomic.AddInt32(&m.closeCalls, 1)
	return nil
}
func (m *wedgedMedia) UnblockReaders() error {
	atomic.AddInt32(&m.unblockCalls, 1)
	return nil
}
func (m *wedgedMedia) OnDTMF(handler voip.DTMFHandler) {}
func (m *wedgedMedia) NegotiatedCodec() voip.CodecInfo { return voip.CodecInfo{} }

// TestReorderBuffer_CloseDoesNotHangOnWedgedInner is the direct fix proof. It sets
// closeDrainTimeout very high, so if Close() only returned via the last-resort
// backstop the test would still be blocked when its own 2s deadline fires. Close()
// returning promptly therefore proves the *real* fix: mainLoop now exits on
// rb.cancel() (the Run() context-wiring fix), independent of the wedged reader, so
// doneCh closes and Close() returns even though ReadRTP never will.
//
// Against the pre-fix code this test HANGS: mainLoop watched the caller's ctx while
// Close() cancelled an unrelated internal ctx, and inner.Close() never woke the
// read, so `<-rb.doneCh` blocked forever.
func TestReorderBuffer_CloseDoesNotHangOnWedgedInner(t *testing.T) {
	restore := closeDrainTimeout
	closeDrainTimeout = 60 * time.Second // prove Close returns via ctx exit, NOT the backstop
	defer func() { closeDrainTimeout = restore }()

	inner := newWedgedMedia()
	rb := NewRTPReorderBuffer(inner, RTPReorderBufferOptions{Depth: 3, MaxWait: 20 * time.Millisecond, CallID: "wedged-1"})
	rb.Run(context.Background())
	// Let the reader reach its blocking ReadRTP before we tear down.
	time.Sleep(20 * time.Millisecond)

	returned := make(chan struct{})
	go func() {
		_ = rb.Close()
		close(returned)
	}()

	select {
	case <-returned:
		// Close returned despite a permanently-wedged inner read, the fix works.
	case <-time.After(2 * time.Second):
		t.Fatal("Close() did not return within 2s on a wedged inner read, teardown deadlock (the ~19h slot-leak hang) is NOT fixed")
	}
	close(inner.block) // let the parked reader exit cleanly for the rest of the run
}

// TestReorderBuffer_CloseReleasesCallSlotEvenWhenInnerWedged models the exact bridge
// structure that leaked: triggerCall.func1 registers `defer releaseCallSlot` and then
// tears the media session down via Close(). The deferred release runs only if Close()
// returns. Here `close(released)` stands in for releaseCallSlot; asserting it fires
// proves the call-slot leak itself is fixed, not merely that Close() unblocks.
func TestReorderBuffer_CloseReleasesCallSlotEvenWhenInnerWedged(t *testing.T) {
	inner := newWedgedMedia()
	rb := NewRTPReorderBuffer(inner, RTPReorderBufferOptions{Depth: 3, MaxWait: 20 * time.Millisecond, CallID: "wedged-slot"})
	rb.Run(context.Background())
	time.Sleep(20 * time.Millisecond)

	released := make(chan struct{})
	go func() {
		defer close(released) // stand-in for `defer c.releaseCallSlot(workspaceID)`
		_ = rb.Close()        // the teardown that used to deadlock, pinning the slot
	}()

	select {
	case <-released:
		// The slot-release defer ran → the workspace call slot is freed. No leak.
	case <-time.After(2 * time.Second):
		t.Fatal("call-slot release defer never ran, Close() hung and the slot would leak forever")
	}

	if atomic.LoadInt32(&inner.unblockCalls) == 0 {
		t.Error("Close() must call inner.UnblockReaders() to wake the wedged reader")
	}
	close(inner.block)
}

// TestReorderBuffer_ConcurrentCloseFromBothPaths mirrors production precisely: pprof
// showed the SAME buffer being Close()'d concurrently from two paths, the bridge
// (RecordingMediaSession.Close) and the SIP dialog teardown (SIPTrunkManager.Invite
// .func1 via diago's OnClose), with BOTH goroutines stuck on `<-doneCh`. Both must
// now return, and the cancel/close/unblock side-effects must run exactly once.
func TestReorderBuffer_ConcurrentCloseFromBothPaths(t *testing.T) {
	inner := newWedgedMedia()
	rb := NewRTPReorderBuffer(inner, RTPReorderBufferOptions{Depth: 3, MaxWait: 20 * time.Millisecond, CallID: "wedged-2paths"})
	rb.Run(context.Background())
	time.Sleep(20 * time.Millisecond)

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = rb.Close()
		}()
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()

	select {
	case <-done:
		// Both teardown paths returned.
	case <-time.After(2 * time.Second):
		t.Fatal("concurrent Close() from both teardown paths did not both return within 2s")
	}

	// closeOnce must guard the one-time side-effects even under two callers.
	if got := atomic.LoadInt32(&inner.unblockCalls); got != 1 {
		t.Errorf("UnblockReaders called %d times, want exactly 1 (closeOnce)", got)
	}
	close(inner.block)
}

// TestReorderBuffer_CloseDrainsCleanlyOnHealthyInner guards the common path: a
// well-behaved inner whose Close unblocks the read must still tear down promptly and
// let the reader exit, i.e. the fix must not have broken normal teardown.
func TestReorderBuffer_CloseDrainsCleanlyOnHealthyInner(t *testing.T) {
	inner := newPassthroughMedia() // Close() closes m.closed → ReadRTP returns
	rb := NewRTPReorderBuffer(inner, RTPReorderBufferOptions{Depth: 3, MaxWait: 20 * time.Millisecond, CallID: "healthy"})
	rb.Run(context.Background())
	time.Sleep(20 * time.Millisecond)

	returned := make(chan struct{})
	go func() {
		_ = rb.Close()
		close(returned)
	}()

	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("Close() on a healthy inner did not return within 2s")
	}
}
