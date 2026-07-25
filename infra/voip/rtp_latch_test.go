package voipinfra

import (
	"encoding/binary"
	"errors"
	"io"
	"log"
	"net"
	"reflect"
	"sync"
	"testing"
	"time"
	"unsafe"

	"github.com/emiago/diago/media"
)

type fakePacket struct {
	data []byte
	src  net.Addr
}

type fakePacketConn struct {
	mu        sync.Mutex
	inbox     []fakePacket
	cond      *sync.Cond
	closed    bool
	writes    []fakePacket
	localAddr net.Addr
}

func newFakePacketConn(local string) *fakePacketConn {
	la, _ := net.ResolveUDPAddr("udp", local)
	c := &fakePacketConn{localAddr: la}
	c.cond = sync.NewCond(&c.mu)
	return c
}

func (c *fakePacketConn) Push(data []byte, src net.Addr) {
	c.mu.Lock()
	c.inbox = append(c.inbox, fakePacket{data: append([]byte(nil), data...), src: src})
	c.cond.Broadcast()
	c.mu.Unlock()
}

func (c *fakePacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for len(c.inbox) == 0 && !c.closed {
		c.cond.Wait()
	}
	if c.closed && len(c.inbox) == 0 {
		return 0, nil, io.EOF
	}
	pkt := c.inbox[0]
	c.inbox = c.inbox[1:]
	n := copy(p, pkt.data)
	return n, pkt.src, nil
}

func (c *fakePacketConn) WriteTo(p []byte, dst net.Addr) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, errors.New("closed")
	}
	c.writes = append(c.writes, fakePacket{data: append([]byte(nil), p...), src: dst})
	return len(p), nil
}

func (c *fakePacketConn) Close() error {
	c.mu.Lock()
	c.closed = true
	c.cond.Broadcast()
	c.mu.Unlock()
	return nil
}
func (c *fakePacketConn) LocalAddr() net.Addr                { return c.localAddr }
func (c *fakePacketConn) SetDeadline(t time.Time) error      { return nil }
func (c *fakePacketConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *fakePacketConn) SetWriteDeadline(t time.Time) error { return nil }

func (c *fakePacketConn) writeCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.writes)
}

func (c *fakePacketConn) lastWriteDst() *net.UDPAddr {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.writes) == 0 {
		return nil
	}
	return c.writes[len(c.writes)-1].src.(*net.UDPAddr)
}

type mockClock struct {
	mu sync.Mutex
	t  time.Time
}

func newMockClock() *mockClock {
	return &mockClock{t: time.Unix(1_700_000_000, 0)}
}
func (c *mockClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}
func (c *mockClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func rtpPacket(version, pt uint8, seq uint16, payloadLen int) []byte {
	pkt := make([]byte, 12+payloadLen)
	pkt[0] = (version & 0x03) << 6
	pkt[1] = pt & 0x7F
	binary.BigEndian.PutUint16(pkt[2:4], seq)
	return pkt
}

func udp(addr string) *net.UDPAddr {
	a, _ := net.ResolveUDPAddr("udp", addr)
	return a
}

func silentLogger() *log.Logger { return log.New(io.Discard, "", 0) }

type testWrapper struct {
	w     *latchingPacketConn
	inner *fakePacketConn
	clock *mockClock
}

func newTestWrapper(t *testing.T, sdpRaddr string, opts ...func(*latchingPacketConn)) *testWrapper {
	t.Helper()
	inner := newFakePacketConn("127.0.0.1:10000")
	clock := newMockClock()
	w := &latchingPacketConn{
		PacketConn:          inner,
		sdpRaddr:            *udp(sdpRaddr),
		expectedPTs:         map[uint8]struct{}{0: {}, 8: {}, 101: {}},
		logger:              silentLogger(),
		minSequential:       defaultMinSequential,
		learningMinDuration: defaultLearningMinDuration,
		learningMaxDuration: defaultLearningMaxDuration,
		now:                 clock.Now,
	}
	for _, o := range opts {
		o(w)
	}
	return &testWrapper{w: w, inner: inner, clock: clock}
}

func (tw *testWrapper) drive(t *testing.T, src *net.UDPAddr, pt uint8, seq uint16, step time.Duration) {
	t.Helper()
	tw.inner.Push(rtpPacket(2, pt, seq, 160), src)
	buf := make([]byte, 1600)
	if _, _, err := tw.w.ReadFrom(buf); err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}
	tw.clock.Advance(step)
}

func (tw *testWrapper) driveSequential(t *testing.T, src *net.UDPAddr, start uint16, n int, step time.Duration) {
	t.Helper()
	for i := 0; i < n; i++ {
		tw.drive(t, src, 0, start+uint16(i), step)
	}
}

func TestProbation_LatchesAfter5SequentialPackets(t *testing.T) {
	tw := newTestWrapper(t, "177.51.138.18:20098")
	src := udp("190.89.250.159:21706")

	tw.driveSequential(t, src, 100, 5, 20*time.Millisecond)

	if !tw.w.latched.Load() {
		t.Fatalf("expected latched=true after 5 sequential packets")
	}
	if latchOutcome(tw.w.outcome.Load()) != latchOutcomeApplied {
		t.Errorf("outcome=%d want Applied", tw.w.outcome.Load())
	}
	red := tw.w.redirectAddr.Load()
	if red == nil || !red.IP.Equal(src.IP) || red.Port != src.Port {
		t.Errorf("redirectAddr=%v want %s", red, src.String())
	}
}

func TestProbation_LatchesExactlyOnThresholdPacket(t *testing.T) {
	tw := newTestWrapper(t, "177.51.138.18:20098")
	src := udp("190.89.250.159:21706")

	tw.driveSequential(t, src, 100, 4, 20*time.Millisecond)
	if tw.w.latched.Load() {
		t.Fatalf("must not latch on packet #4 (need 5 = seed + 4 followups)")
	}

	tw.driveSequential(t, src, 104, 1, 20*time.Millisecond)
	if !tw.w.latched.Load() {
		t.Fatalf("must latch on packet #5")
	}
}

func TestProbation_CrossNetworkLogsAdvisory(t *testing.T) {
	var buf logCapture
	tw := newTestWrapper(t, "177.51.138.18:20098", func(w *latchingPacketConn) {
		w.logger = log.New(&buf, "", 0)
	})
	src := udp("190.89.250.159:21706")
	tw.driveSequential(t, src, 100, 5, 20*time.Millisecond)

	if !buf.contains("cross-network") {
		t.Errorf("expected cross-network advisory log, got: %s", buf.String())
	}
}

func TestProbation_SameNetworkUsesStandardLog(t *testing.T) {
	var buf logCapture
	tw := newTestWrapper(t, "177.51.138.18:20098", func(w *latchingPacketConn) {
		w.logger = log.New(&buf, "", 0)
	})
	src := udp("177.51.138.20:29830")
	tw.driveSequential(t, src, 100, 5, 20*time.Millisecond)

	if buf.contains("cross-network") {
		t.Errorf("did not expect cross-network advisory: %s", buf.String())
	}
	if !buf.contains("RTP LATCHED") {
		t.Errorf("expected RTP LATCHED log, got: %s", buf.String())
	}
}

func TestProbation_CompetingSourceResets(t *testing.T) {
	tw := newTestWrapper(t, "177.51.138.18:20098")
	srcA := udp("190.89.250.159:21706")
	srcB := udp("190.89.250.160:11111")

	tw.driveSequential(t, srcA, 100, 2, 20*time.Millisecond)
	tw.driveSequential(t, srcB, 500, 5, 20*time.Millisecond)

	if !tw.w.latched.Load() {
		t.Fatalf("expected latched=true after B's full probation")
	}
	red := tw.w.redirectAddr.Load()
	if red.Port != srcB.Port {
		t.Errorf("latched to port=%d want %d (B)", red.Port, srcB.Port)
	}
}

func TestProbation_OutOfOrderSequenceResets(t *testing.T) {
	tw := newTestWrapper(t, "177.51.138.18:20098")
	src := udp("190.89.250.159:21706")

	tw.driveSequential(t, src, 100, 3, 20*time.Millisecond)
	tw.drive(t, src, 0, 105, 20*time.Millisecond)

	if tw.w.latched.Load() {
		t.Fatalf("out-of-order packet must reset probation, not latch")
	}

	tw.driveSequential(t, src, 106, 4, 20*time.Millisecond)
	if !tw.w.latched.Load() {
		t.Errorf("expected latch after restart with new sequential run")
	}
}

func TestProbation_WindowExpiryReseeds(t *testing.T) {
	tw := newTestWrapper(t, "177.51.138.18:20098")
	src := udp("190.89.250.159:21706")

	tw.drive(t, src, 0, 100, defaultLearningMaxDuration+time.Second)

	tw.driveSequential(t, src, 500, 5, 20*time.Millisecond)
	if !tw.w.latched.Load() {
		t.Errorf("expected latch after window-expired re-seed completed probation")
	}
}

func TestProbation_FloodGuardRestartsProbation(t *testing.T) {
	var buf logCapture
	tw := newTestWrapper(t, "177.51.138.18:20098", func(w *latchingPacketConn) {
		w.logger = log.New(&buf, "", 0)
	})
	src := udp("190.89.250.159:21706")

	for i := 0; i < 5; i++ {
		tw.drive(t, src, 0, 100+uint16(i), 1*time.Millisecond)
	}
	if tw.w.latched.Load() {
		t.Errorf("flood (5 pkts in 4ms) must not latch")
	}
	if !buf.contains("flood-guarded") {
		t.Errorf("expected flood-guarded log, got: %s", buf.String())
	}

	tw.driveSequential(t, src, 200, 5, 20*time.Millisecond)
	if !tw.w.latched.Load() {
		t.Errorf("expected latch after flood reset + normal probation")
	}
}

func TestProbation_NoopWhenSourceMatchesSDP(t *testing.T) {
	tw := newTestWrapper(t, "177.51.138.18:20098")
	src := udp("177.51.138.18:20098")

	tw.driveSequential(t, src, 100, 5, 20*time.Millisecond)

	if !tw.w.latched.Load() {
		t.Errorf("expected latched=true (Noop is still a terminal state)")
	}
	if latchOutcome(tw.w.outcome.Load()) != latchOutcomeNoop {
		t.Errorf("outcome=%d want Noop", tw.w.outcome.Load())
	}
	if tw.w.redirectAddr.Load() != nil {
		t.Errorf("redirectAddr must remain nil for Noop")
	}
}

func TestSoftGate_ShortPacketIgnored(t *testing.T) {
	tw := newTestWrapper(t, "177.51.138.18:20098")
	src := udp("190.89.250.159:21706")

	tw.inner.Push([]byte{1, 2, 3}, src)
	buf := make([]byte, 1600)
	_, _, _ = tw.w.ReadFrom(buf)

	tw.driveSequential(t, src, 100, 5, 20*time.Millisecond)
	if !tw.w.latched.Load() {
		t.Errorf("short packet must not freeze; later probation must complete")
	}
}

func TestSoftGate_WrongRTPVersionIgnored(t *testing.T) {
	tw := newTestWrapper(t, "177.51.138.18:20098")
	src := udp("190.89.250.159:21706")

	tw.inner.Push(rtpPacket(1, 0, 100, 160), src)
	buf := make([]byte, 1600)
	_, _, _ = tw.w.ReadFrom(buf)
	tw.driveSequential(t, src, 100, 5, 20*time.Millisecond)
	if !tw.w.latched.Load() {
		t.Errorf("v=1 packet must not freeze probation")
	}
}

func TestSoftGate_UnexpectedPTIgnored(t *testing.T) {
	tw := newTestWrapper(t, "177.51.138.18:20098")
	src := udp("190.89.250.159:21706")

	tw.inner.Push(rtpPacket(2, 96, 100, 160), src)
	buf := make([]byte, 1600)
	_, _, _ = tw.w.ReadFrom(buf)
	tw.driveSequential(t, src, 100, 5, 20*time.Millisecond)
	if !tw.w.latched.Load() {
		t.Errorf("unexpected PT must not freeze probation")
	}
}

func TestSoftGate_NonUDPAddrIgnored(t *testing.T) {
	tw := newTestWrapper(t, "177.51.138.18:20098")
	tcp, _ := net.ResolveTCPAddr("tcp", "190.89.250.159:21706")

	tw.inner.Push(rtpPacket(2, 0, 100, 160), tcp)
	buf := make([]byte, 1600)
	_, _, _ = tw.w.ReadFrom(buf)
	if tw.w.latched.Load() {
		t.Errorf("non-UDP addr must not advance probation")
	}
}

func TestSoftGate_NilAddrIgnored(t *testing.T) {
	tw := newTestWrapper(t, "177.51.138.18:20098")

	tw.inner.Push(rtpPacket(2, 0, 100, 160), nil)
	buf := make([]byte, 1600)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic on nil addr: %v", r)
		}
	}()
	_, _, _ = tw.w.ReadFrom(buf)
	if tw.w.latched.Load() {
		t.Errorf("nil addr must not advance probation")
	}
}

func TestSoftGate_UDPAddrWithNilIPIgnored(t *testing.T) {
	tw := newTestWrapper(t, "177.51.138.18:20098")

	tw.inner.Push(rtpPacket(2, 0, 100, 160), &net.UDPAddr{Port: 12345})
	buf := make([]byte, 1600)
	_, _, _ = tw.w.ReadFrom(buf)
	if tw.w.latched.Load() {
		t.Errorf("UDP addr with nil IP must not advance probation")
	}
}

func TestSoftGate_IPv6SourceIgnored(t *testing.T) {
	tw := newTestWrapper(t, "177.51.138.18:20098")
	v6, _ := net.ResolveUDPAddr("udp", "[2001:db8::1]:29830")
	tw.inner.Push(rtpPacket(2, 0, 100, 160), v6)
	buf := make([]byte, 1600)
	_, _, _ = tw.w.ReadFrom(buf)
	if tw.w.latched.Load() {
		t.Errorf("IPv6 source must not advance probation")
	}
}

func TestSourceAcceptable_PublicSDP(t *testing.T) {
	tw := newTestWrapper(t, "177.51.138.18:20098")
	cases := []struct {
		ip   string
		want bool
	}{
		{"190.89.250.159", true},
		{"177.51.138.20", true},
		{"127.0.0.1", false},
		{"224.0.0.1", false},
		{"0.0.0.0", false},
		{"255.255.255.255", false},
		{"10.0.0.1", false},
		{"192.168.1.5", false},
		{"172.16.0.1", false},
		{"169.254.0.1", false},
	}
	for _, tc := range cases {
		got := tw.w.sourceAcceptable(net.ParseIP(tc.ip).To4())
		if got != tc.want {
			t.Errorf("sourceAcceptable(%s) public-sdp = %v want %v", tc.ip, got, tc.want)
		}
	}
}

func TestSourceAcceptable_PrivateSDP_PermitsAnything(t *testing.T) {
	tw := newTestWrapper(t, "192.168.1.10:20098")
	cases := []struct {
		ip   string
		want bool
	}{
		{"192.168.1.5", true},
		{"10.0.0.1", true},
		{"169.254.0.1", true},
		{"127.0.0.1", false},
		{"224.0.0.1", false},
		{"190.89.250.159", true},
	}
	for _, tc := range cases {
		got := tw.w.sourceAcceptable(net.ParseIP(tc.ip).To4())
		if got != tc.want {
			t.Errorf("sourceAcceptable(%s) private-sdp = %v want %v", tc.ip, got, tc.want)
		}
	}
}

func TestSourceAcceptable_LoopbackSDPStillRejectsLoopbackSource(t *testing.T) {

	tw := newTestWrapper(t, "127.0.0.1:20098")
	if tw.w.sourceAcceptable(net.ParseIP("127.0.0.2").To4()) {
		t.Errorf("loopback source must be rejected regardless of SDP")
	}
}

func TestProbation_PrivateSourceWithPublicSDPDoesNotLatch(t *testing.T) {
	tw := newTestWrapper(t, "177.51.138.18:20098")
	attacker := udp("10.0.0.5:31337")

	tw.driveSequential(t, attacker, 100, 10, 20*time.Millisecond)

	if tw.w.latched.Load() {
		t.Fatalf("private-source flood against public SDP must NOT latch")
	}
}

func TestLatch_OneShotFreezesAfterApply(t *testing.T) {
	tw := newTestWrapper(t, "177.51.138.18:20098")
	srcA := udp("190.89.250.159:21706")
	srcB := udp("190.89.250.160:11111")

	tw.driveSequential(t, srcA, 100, 5, 20*time.Millisecond)
	if !tw.w.latched.Load() {
		t.Fatalf("setup failure: A did not latch")
	}
	tw.driveSequential(t, srcB, 200, 10, 20*time.Millisecond)

	red := tw.w.redirectAddr.Load()
	if red.Port != srcA.Port {
		t.Errorf("relatched to B (port %d), expected sticky A (port %d)",
			red.Port, srcA.Port)
	}
}

func TestWriteTo_PassthroughWhenNotLatched(t *testing.T) {
	tw := newTestWrapper(t, "177.51.138.18:20098")
	dst := udp("177.51.138.18:20098")
	if _, err := tw.w.WriteTo([]byte("hi"), dst); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	got := tw.inner.lastWriteDst()
	if got == nil || got.Port != dst.Port {
		t.Errorf("write not passed through; got %v", got)
	}
}

func TestWriteTo_RedirectsAfterLatch(t *testing.T) {
	tw := newTestWrapper(t, "177.51.138.18:20098")
	src := udp("190.89.250.159:21706")
	tw.driveSequential(t, src, 100, 5, 20*time.Millisecond)
	if !tw.w.latched.Load() {
		t.Fatalf("setup: not latched")
	}

	sdpDst := udp("177.51.138.18:20098")
	if _, err := tw.w.WriteTo([]byte("audio"), sdpDst); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	got := tw.inner.lastWriteDst()
	if !got.IP.Equal(src.IP) || got.Port != src.Port {
		t.Errorf("redirect not applied: got=%v want=%v", got, src)
	}
}

func TestLatch_DisabledNeverLatches(t *testing.T) {
	tw := newTestWrapper(t, "177.51.138.18:20098", func(w *latchingPacketConn) {
		w.disabled = true
	})
	src := udp("190.89.250.159:21706")
	tw.driveSequential(t, src, 100, 10, 20*time.Millisecond)
	if tw.w.latched.Load() {
		t.Errorf("disabled wrapper must not latch")
	}
}

func TestLatch_PropagatesReadError(t *testing.T) {
	tw := newTestWrapper(t, "177.51.138.18:20098")
	tw.inner.Close()
	buf := make([]byte, 1600)
	_, _, err := tw.w.ReadFrom(buf)
	if !errors.Is(err, io.EOF) {
		t.Errorf("err=%v want io.EOF", err)
	}
	if tw.w.latched.Load() {
		t.Errorf("read error must not flip latched")
	}
}

func TestLatch_ConcurrentReadsLatchExactlyOnce(t *testing.T) {
	tw := newTestWrapper(t, "177.51.138.18:20098")

	tw.w.learningMinDuration = 0
	src := udp("190.89.250.159:21706")

	const nGoroutines = 8
	const readsPerGoroutine = 13
	for i := 0; i < nGoroutines*readsPerGoroutine; i++ {
		tw.inner.Push(rtpPacket(2, 0, 100+uint16(i), 160), src)
	}

	var wg sync.WaitGroup
	for i := 0; i < nGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			buf := make([]byte, 1600)
			for j := 0; j < readsPerGoroutine; j++ {
				_, _, _ = tw.w.ReadFrom(buf)
			}
		}()
	}
	wg.Wait()

	if !tw.w.latched.Load() {
		t.Errorf("expected latched=true after 100 sequentials drained")
	}
	red := tw.w.redirectAddr.Load()
	if red == nil || red.Port != src.Port {
		t.Errorf("redirectAddr=%v want :%d", red, src.Port)
	}
}

func TestCommitLatch_CASFalseBranchIsNoOp(t *testing.T) {
	tw := newTestWrapper(t, "177.51.138.18:20098")
	tw.w.latched.Store(true)
	tw.w.outcome.Store(int32(latchOutcomeApplied))

	src := udp("190.89.250.160:11111")
	tw.w.commitLatch(src, src.IP.To4())

	if tw.w.redirectAddr.Load() != nil {
		t.Errorf("commitLatch must not install redirect when CAS fails")
	}
}

func TestSameUDPAddr(t *testing.T) {
	a := udp("1.2.3.4:5060")
	cases := []struct {
		x, y *net.UDPAddr
		want bool
	}{
		{a, a, true},
		{a, udp("1.2.3.4:5060"), true},
		{a, udp("1.2.3.4:5061"), false},
		{a, udp("1.2.3.5:5060"), false},
		{nil, a, false},
		{a, nil, false},
	}
	for _, tc := range cases {
		if got := sameUDPAddr(tc.x, tc.y); got != tc.want {
			t.Errorf("sameUDPAddr(%v,%v)=%v want %v", tc.x, tc.y, got, tc.want)
		}
	}
}

func TestSameNetwork16(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"177.51.138.18", "177.51.138.19", true},
		{"177.51.0.1", "177.51.255.254", true},
		{"177.51.138.18", "177.52.138.18", false},
		{"190.89.250.159", "177.51.138.18", false},
	}
	for _, tc := range cases {
		got := sameNetwork16(net.ParseIP(tc.a).To4(), net.ParseIP(tc.b).To4())
		if got != tc.want {
			t.Errorf("sameNetwork16(%s,%s)=%v want %v", tc.a, tc.b, got, tc.want)
		}
	}
	if sameNetwork16(net.ParseIP("::1"), net.ParseIP("::2")) {
		t.Errorf("IPv6 inputs should return false")
	}
	if sameNetwork16(nil, nil) {
		t.Errorf("nil inputs should return false")
	}
}

func TestPayloadTypesFromCodecs(t *testing.T) {

	got := PayloadTypesFromCodecs([]media.Codec{
		{PayloadType: 0, Name: "PCMU"},
		{PayloadType: 8, Name: "PCMA"},
		{PayloadType: 0, Name: "PCMU-dup"},
	})
	if !reflect.DeepEqual(got, []uint8{0, 8, 101}) {
		t.Errorf("got %v want [0 8 101]", got)
	}

	got = PayloadTypesFromCodecs([]media.Codec{
		{PayloadType: 0}, {PayloadType: 101},
	})
	if !reflect.DeepEqual(got, []uint8{0, 101, 8}) {
		t.Errorf("got %v want [0 101 8]", got)
	}

	got = PayloadTypesFromCodecs(nil)
	if !reflect.DeepEqual(got, []uint8{0, 8, 101}) {
		t.Errorf("got %v want [0 8 101]", got)
	}

	got = PayloadTypesFromCodecs([]media.Codec{{PayloadType: 111, Name: "opus"}})
	if !reflect.DeepEqual(got, []uint8{111, 0, 8, 101}) {
		t.Errorf("got %v want [111 0 8 101]", got)
	}
}

func TestLatchingPacketConn_PassthroughMethods(t *testing.T) {
	tw := newTestWrapper(t, "177.51.138.18:20098")
	if tw.w.LocalAddr() == nil {
		t.Errorf("LocalAddr lost")
	}
	if err := tw.w.SetDeadline(time.Time{}); err != nil {
		t.Errorf("SetDeadline: %v", err)
	}
	if err := tw.w.SetReadDeadline(time.Time{}); err != nil {
		t.Errorf("SetReadDeadline: %v", err)
	}
	if err := tw.w.SetWriteDeadline(time.Time{}); err != nil {
		t.Errorf("SetWriteDeadline: %v", err)
	}
	if err := tw.w.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestLatch_LogfNilLoggerUsesDefault(t *testing.T) {

	w := &latchingPacketConn{}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("logf with nil logger panicked: %v", r)
		}
	}()
	w.logf("test %s", "msg")
}

func TestWrap_NilSession(t *testing.T) {
	err := WrapMediaSessionForLatching(nil, LatchOptions{ExpectedPTs: []uint8{0}})
	if err == nil {
		t.Fatalf("expected error on nil session")
	}
}

func TestWrap_NoPTsRefused(t *testing.T) {
	sess := newRealInitializedSession(t, "177.51.138.18:20098")
	defer sess.Close()
	err := WrapMediaSessionForLatching(sess, LatchOptions{})
	if err == nil {
		t.Fatalf("expected error when ExpectedPTs is empty")
	}
}

func TestWrap_DisabledIsNoop(t *testing.T) {
	sess := newRealInitializedSession(t, "177.51.138.18:20098")
	defer sess.Close()
	before, _, _ := readRtpConnField(sess)
	if err := WrapMediaSessionForLatching(sess, LatchOptions{
		ExpectedPTs: []uint8{0},
		Disabled:    true,
	}); err != nil {
		t.Fatalf("Wrap returned err: %v", err)
	}
	after, _, _ := readRtpConnField(sess)
	if before != after {
		t.Errorf("rtpConn must not change when Disabled=true")
	}
}

func TestWrap_EnvDisabledIsNoop(t *testing.T) {
	orig := envDisabled
	envDisabled = true
	defer func() { envDisabled = orig }()

	sess := newRealInitializedSession(t, "177.51.138.18:20098")
	defer sess.Close()
	before, _, _ := readRtpConnField(sess)
	if err := WrapMediaSessionForLatching(sess, LatchOptions{
		ExpectedPTs: []uint8{0},
	}); err != nil {
		t.Fatalf("Wrap returned err: %v", err)
	}
	after, _, _ := readRtpConnField(sess)
	if before != after {
		t.Errorf("env-disabled must short-circuit wrap")
	}
}

func TestWrap_IdempotentDoubleWrap(t *testing.T) {
	sess := newRealInitializedSession(t, "177.51.138.18:20098")
	defer sess.Close()

	if err := WrapMediaSessionForLatching(sess, LatchOptions{ExpectedPTs: []uint8{0}}); err != nil {
		t.Fatalf("first wrap: %v", err)
	}
	c1, _, _ := readRtpConnField(sess)
	if err := WrapMediaSessionForLatching(sess, LatchOptions{ExpectedPTs: []uint8{0}}); err != nil {
		t.Fatalf("second wrap: %v", err)
	}
	c2, _, _ := readRtpConnField(sess)
	if c1 != c2 {
		t.Errorf("second wrap should be no-op, but rtpConn changed")
	}
}

func TestWrap_NilRtpConnReturnsError(t *testing.T) {

	sess := &media.MediaSession{}
	sess.SetRemoteAddr(udp("177.51.138.18:20098"))

	err := WrapMediaSessionForLatching(sess, LatchOptions{ExpectedPTs: []uint8{0}})
	if err == nil {
		t.Fatalf("expected error when rtpConn is nil")
	}
}

func TestWrap_OptionDefaultsApplied(t *testing.T) {
	sess := newRealInitializedSession(t, "177.51.138.18:20098")
	defer sess.Close()
	if err := WrapMediaSessionForLatching(sess, LatchOptions{
		ExpectedPTs: []uint8{0},
		Logger:      silentLogger(),
	}); err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	wrapper, _, _ := readRtpConnField(sess)
	lpc := wrapper.(*latchingPacketConn)
	if lpc.minSequential != defaultMinSequential {
		t.Errorf("minSequential=%d want default %d", lpc.minSequential, defaultMinSequential)
	}
	if lpc.learningMinDuration != defaultLearningMinDuration {
		t.Errorf("learningMinDuration default not applied")
	}
	if lpc.learningMaxDuration != defaultLearningMaxDuration {
		t.Errorf("learningMaxDuration default not applied")
	}
	if lpc.now == nil {
		t.Errorf("now func default not applied")
	}
}

func TestWrap_OptionOverridesApplied(t *testing.T) {
	sess := newRealInitializedSession(t, "177.51.138.18:20098")
	defer sess.Close()
	clock := newMockClock()
	if err := WrapMediaSessionForLatching(sess, LatchOptions{
		ExpectedPTs:         []uint8{0},
		Logger:              silentLogger(),
		MinSequential:       7,
		LearningMinDuration: 99 * time.Millisecond,
		LearningMaxDuration: 5 * time.Second,
		NowFunc:             clock.Now,
	}); err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	wrapper, _, _ := readRtpConnField(sess)
	lpc := wrapper.(*latchingPacketConn)
	if lpc.minSequential != 7 {
		t.Errorf("minSequential=%d want 7", lpc.minSequential)
	}
	if lpc.learningMinDuration != 99*time.Millisecond {
		t.Errorf("learningMinDuration=%v want 99ms", lpc.learningMinDuration)
	}
	if lpc.learningMaxDuration != 5*time.Second {
		t.Errorf("learningMaxDuration=%v want 5s", lpc.learningMaxDuration)
	}

	if !lpc.now().Equal(clock.Now()) {
		t.Errorf("now func not wired")
	}
}

func newRealInitializedSession(t *testing.T, raddr string) *media.MediaSession {
	t.Helper()
	sess := &media.MediaSession{}
	sess.SetRemoteAddr(udp(raddr))
	inner := newFakePacketConn("127.0.0.1:10000")

	v := reflect.ValueOf(sess).Elem()
	f := v.FieldByName(rtpConnFieldName)
	if !f.IsValid() {
		t.Fatalf("diago MediaSession has no field named %q", rtpConnFieldName)
	}
	pcType := reflect.TypeOf((*net.PacketConn)(nil)).Elem()
	if f.Type() != pcType {
		t.Fatalf("rtpConn type changed: got %s want net.PacketConn", f.Type())
	}
	addr := unsafe.Pointer(f.UnsafeAddr())
	*(*net.PacketConn)(addr) = inner
	return sess
}

func TestWrap_EndToEnd_LatchesOnRealSession(t *testing.T) {
	sess := newRealInitializedSession(t, "177.51.138.18:20098")
	defer sess.Close()

	clock := newMockClock()
	if err := WrapMediaSessionForLatching(sess, LatchOptions{
		ExpectedPTs: []uint8{0, 101},
		TrunkID:     "trunk-A",
		CallID:      "call-1",
		Logger:      silentLogger(),
		NowFunc:     clock.Now,
	}); err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	wrapper, _, _ := readRtpConnField(sess)
	lpc := wrapper.(*latchingPacketConn)
	inner := lpc.PacketConn.(*fakePacketConn)

	src := udp("190.89.250.159:21706")
	for i := 0; i < 5; i++ {
		inner.Push(rtpPacket(2, 0, 100+uint16(i), 160), src)
	}
	buf := make([]byte, 1600)
	for i := 0; i < 5; i++ {
		if _, _, err := wrapper.ReadFrom(buf); err != nil {
			t.Fatalf("ReadFrom: %v", err)
		}
		clock.Advance(20 * time.Millisecond)
	}

	if !lpc.latched.Load() {
		t.Fatalf("not latched after 5 sequentials")
	}
	red := lpc.redirectAddr.Load()
	if !red.IP.Equal(src.IP) || red.Port != src.Port {
		t.Errorf("redirectAddr=%s want %s", red, src)
	}
	if !sess.Raddr.IP.Equal(net.ParseIP("177.51.138.18").To4()) || sess.Raddr.Port != 20098 {
		t.Errorf("sess.Raddr mutated: %s", sess.Raddr.String())
	}
	if _, err := wrapper.WriteTo([]byte("audio"), &sess.Raddr); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	got := inner.lastWriteDst()
	if !got.IP.Equal(src.IP) || got.Port != src.Port {
		t.Errorf("WriteTo not redirected; dst=%s", got)
	}
}

func TestWriteRtpConnField_NilAddrReturnsError(t *testing.T) {
	if err := writeRtpConnField(nil, nil); !errors.Is(err, ErrLatchUnavailable) {
		t.Errorf("err=%v want ErrLatchUnavailable", err)
	}
}

func TestDiagoMediaSessionLayoutAssumption(t *testing.T) {
	sess := &media.MediaSession{}
	v := reflect.ValueOf(sess).Elem()
	f := v.FieldByName(rtpConnFieldName)
	if !f.IsValid() {
		t.Fatalf("upstream change: media.MediaSession has no %q field", rtpConnFieldName)
	}
	if f.Type().String() != "net.PacketConn" {
		t.Fatalf("upstream change: media.MediaSession.%s is %s (want net.PacketConn)",
			rtpConnFieldName, f.Type())
	}
}

type logCapture struct {
	mu  sync.Mutex
	buf []byte
}

func (b *logCapture) Write(p []byte) (int, error) {
	b.mu.Lock()
	b.buf = append(b.buf, p...)
	b.mu.Unlock()
	return len(p), nil
}
func (b *logCapture) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}
func (b *logCapture) contains(s string) bool {
	return indexOf(b.String(), s) >= 0
}

func indexOf(haystack, needle string) int {
	if len(needle) == 0 {
		return 0
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
