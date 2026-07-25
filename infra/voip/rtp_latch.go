package voipinfra

import (
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/emiago/diago/media"
)

const (
	defaultMinSequential       = 4
	defaultLearningMinDuration = 56 * time.Millisecond
	defaultLearningMaxDuration = 3 * time.Second
)

type LatchOptions struct {
	ExpectedPTs []uint8

	TrunkID string
	CallID  string

	Logger *log.Logger

	Disabled bool

	MinSequential       int
	LearningMinDuration time.Duration
	LearningMaxDuration time.Duration

	NowFunc func() time.Time
}

type latchOutcome int32

const (
	latchOutcomePending latchOutcome = 0
	latchOutcomeApplied latchOutcome = 1
	latchOutcomeNoop    latchOutcome = 2
)

type latchingPacketConn struct {
	net.PacketConn

	sdpRaddr    net.UDPAddr
	expectedPTs map[uint8]struct{}
	trunkID     string
	callID      string
	logger      *log.Logger
	disabled    bool

	minSequential       int
	learningMinDuration time.Duration
	learningMaxDuration time.Duration
	now                 func() time.Time

	latched atomic.Bool

	outcome atomic.Int32

	redirectAddr atomic.Pointer[net.UDPAddr]

	mu              sync.Mutex
	candidate       *net.UDPAddr
	lastSeq         uint16
	sequentialLeft  int
	learningStarted time.Time

	diagReads       atomic.Uint64
	diagFirstLogged atomic.Bool
	diagShortPkt    atomic.Uint64
	diagBadVersion  atomic.Uint64
	diagWrongPT     atomic.Uint64
	diagNotUDP      atomic.Uint64
	diagIPv6        atomic.Uint64
	diagSrcRejected atomic.Uint64
	diagSeeds       atomic.Uint64
	diagAdvances    atomic.Uint64
	diagWrites      atomic.Uint64
	diagWriteFirst  atomic.Bool
}

func (l *latchingPacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	n, addr, err := l.PacketConn.ReadFrom(p)
	if err != nil {
		return n, addr, err
	}
	if l.latched.Load() || l.disabled {
		return n, addr, err
	}
	reads := l.diagReads.Add(1)

	if !l.diagFirstLogged.Swap(true) {
		var pt uint8
		var ver, seq uint32
		if n >= 12 {
			ver = uint32(p[0] >> 6)
			pt = p[1] & 0x7F
			seq = uint32(binary.BigEndian.Uint16(p[2:4]))
		}
		l.logf("first inbound: src=%v len=%d ver=%d pt=%d seq=%d trunk=%s call=%s",
			addr, n, ver, pt, seq, l.trunkID, l.callID)
	}
	l.evaluate(p[:n], addr)

	if reads%50 == 0 && !l.latched.Load() {
		l.logf("still probating: reads=%d shortPkt=%d badVer=%d wrongPT=%d notUDP=%d ipv6=%d srcRej=%d seeds=%d adv=%d trunk=%s call=%s",
			reads,
			l.diagShortPkt.Load(), l.diagBadVersion.Load(),
			l.diagWrongPT.Load(), l.diagNotUDP.Load(),
			l.diagIPv6.Load(), l.diagSrcRejected.Load(),
			l.diagSeeds.Load(), l.diagAdvances.Load(),
			l.trunkID, l.callID)
	}
	return n, addr, err
}

func (l *latchingPacketConn) WriteTo(p []byte, dst net.Addr) (int, error) {
	writes := l.diagWrites.Add(1)
	if !l.diagWriteFirst.Swap(true) {
		var la string
		if a := l.PacketConn.LocalAddr(); a != nil {
			la = a.String()
		}
		l.logf("first WriteTo: localAddr=%s dst=%v len=%d redirectInstalled=%v trunk=%s call=%s",
			la, dst, len(p), l.redirectAddr.Load() != nil, l.trunkID, l.callID)
	}
	if r := l.redirectAddr.Load(); r != nil {
		return l.PacketConn.WriteTo(p, r)
	}
	_ = writes
	return l.PacketConn.WriteTo(p, dst)
}

func (l *latchingPacketConn) evaluate(pkt []byte, addr net.Addr) {
	if len(pkt) < 12 {
		l.diagShortPkt.Add(1)
		return
	}
	if pkt[0]>>6 != 2 {
		l.diagBadVersion.Add(1)
		return
	}
	pt := pkt[1] & 0x7F
	if _, ok := l.expectedPTs[pt]; !ok {
		l.diagWrongPT.Add(1)
		return
	}
	udpSrc, ok := addr.(*net.UDPAddr)
	if !ok || udpSrc == nil || udpSrc.IP == nil {
		l.diagNotUDP.Add(1)
		return
	}
	srcIP4 := udpSrc.IP.To4()
	if srcIP4 == nil {
		l.diagIPv6.Add(1)
		return
	}
	if !l.sourceAcceptable(srcIP4) {
		l.diagSrcRejected.Add(1)
		return
	}

	seq := binary.BigEndian.Uint16(pkt[2:4])
	now := l.now()

	l.mu.Lock()
	defer l.mu.Unlock()

	if l.latched.Load() {
		return
	}

	if l.candidate == nil {
		l.seedCandidate(udpSrc, srcIP4, seq, now)
		return
	}

	if !sameUDPAddr(l.candidate, udpSrc) {
		l.seedCandidate(udpSrc, srcIP4, seq, now)
		return
	}

	if now.Sub(l.learningStarted) > l.learningMaxDuration {
		l.seedCandidate(udpSrc, srcIP4, seq, now)
		return
	}

	if seq != l.lastSeq+1 {
		l.seedCandidate(udpSrc, srcIP4, seq, now)
		return
	}

	l.lastSeq = seq
	l.sequentialLeft--
	l.diagAdvances.Add(1)
	if l.sequentialLeft > 0 {
		return
	}

	elapsed := now.Sub(l.learningStarted)
	if elapsed < l.learningMinDuration {
		l.logf("latch probation flood-guarded: src=%s elapsed=%s min=%s trunk=%s call=%s",
			udpSrc.String(), elapsed, l.learningMinDuration, l.trunkID, l.callID)
		l.candidate = nil
		return
	}

	l.commitLatch(udpSrc, srcIP4)
}

func (l *latchingPacketConn) seedCandidate(udpSrc *net.UDPAddr, ip4 net.IP, seq uint16, now time.Time) {
	l.candidate = &net.UDPAddr{IP: append(net.IP(nil), ip4...), Port: udpSrc.Port}
	l.lastSeq = seq
	l.sequentialLeft = l.minSequential
	l.learningStarted = now
	l.diagSeeds.Add(1)
}

func (l *latchingPacketConn) commitLatch(udpSrc *net.UDPAddr, ip4 net.IP) {

	if !l.latched.CompareAndSwap(false, true) {
		return
	}
	sdpIP4 := l.sdpRaddr.IP.To4()
	if sdpIP4 != nil && ip4.Equal(sdpIP4) && udpSrc.Port == l.sdpRaddr.Port {
		l.outcome.Store(int32(latchOutcomeNoop))
		l.logf("latch noop: src already matches SDP src=%s trunk=%s call=%s",
			udpSrc.String(), l.trunkID, l.callID)
		return
	}
	newAddr := &net.UDPAddr{IP: append(net.IP(nil), ip4...), Port: udpSrc.Port}
	l.redirectAddr.Store(newAddr)
	l.outcome.Store(int32(latchOutcomeApplied))

	if sdpIP4 != nil && !sameNetwork16(sdpIP4, ip4) {
		l.logf("RTP LATCHED [cross-network — carrier SBC pattern]: sdp=%s actual=%s trunk=%s call=%s",
			l.sdpRaddr.String(), newAddr.String(), l.trunkID, l.callID)
	} else {
		l.logf("RTP LATCHED: sdp=%s actual=%s trunk=%s call=%s",
			l.sdpRaddr.String(), newAddr.String(), l.trunkID, l.callID)
	}
}

func (l *latchingPacketConn) sourceAcceptable(srcIP4 net.IP) bool {
	if srcIP4.IsLoopback() || srcIP4.IsMulticast() ||
		srcIP4.IsUnspecified() || srcIP4.Equal(net.IPv4bcast) {
		return false
	}
	sdpIP4 := l.sdpRaddr.IP.To4()
	sdpIsPrivate := sdpIP4 != nil && (sdpIP4.IsPrivate() ||
		sdpIP4.IsLoopback() || sdpIP4.IsLinkLocalUnicast())
	if sdpIsPrivate {
		return true
	}
	if srcIP4.IsPrivate() || srcIP4.IsLinkLocalUnicast() {
		return false
	}
	return true
}

func sameUDPAddr(a, b *net.UDPAddr) bool {
	if a == nil || b == nil {
		return false
	}
	if a.Port != b.Port {
		return false
	}
	return a.IP.Equal(b.IP)
}

func sameNetwork16(a, b net.IP) bool {
	if len(a) != 4 || len(b) != 4 {
		return false
	}
	return a[0] == b[0] && a[1] == b[1]
}

func (l *latchingPacketConn) logf(format string, args ...interface{}) {
	logger := l.logger
	if logger == nil {
		logger = log.Default()
	}
	logger.Printf("[RTP-latch] "+format, args...)
}

var ErrLatchUnavailable = errors.New("rtp latch: diago MediaSession layout incompatible")

var envDisabled = func() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("RTP_LATCH_DISABLED")))
	return v == "1" || v == "true" || v == "yes"
}()

func WrapMediaSessionForLatching(sess *media.MediaSession, opts LatchOptions) error {
	if sess == nil {
		return errors.New("rtp latch: nil media session")
	}
	if opts.Disabled || envDisabled {
		return nil
	}
	if len(opts.ExpectedPTs) == 0 {
		return errors.New("rtp latch: no expected payload types provided")
	}

	sdpRaddr := sess.Raddr

	ptSet := make(map[uint8]struct{}, len(opts.ExpectedPTs))
	for _, pt := range opts.ExpectedPTs {
		ptSet[pt] = struct{}{}
	}

	cur, addr, err := readRtpConnField(sess)
	if err != nil {
		return err
	}
	if cur == nil {
		return fmt.Errorf("rtp latch: rtpConn is nil (session not initialized)")
	}
	if _, already := cur.(*latchingPacketConn); already {
		return nil
	}

	minSeq := opts.MinSequential
	if minSeq <= 0 {
		minSeq = defaultMinSequential
	}
	minDur := opts.LearningMinDuration
	if minDur <= 0 {
		minDur = defaultLearningMinDuration
	}
	maxDur := opts.LearningMaxDuration
	if maxDur <= 0 {
		maxDur = defaultLearningMaxDuration
	}
	nowFn := opts.NowFunc
	if nowFn == nil {
		nowFn = time.Now
	}

	wrapper := &latchingPacketConn{
		PacketConn:          cur,
		sdpRaddr:            sdpRaddr,
		expectedPTs:         ptSet,
		trunkID:             opts.TrunkID,
		callID:              opts.CallID,
		logger:              opts.Logger,
		disabled:            false,
		minSequential:       minSeq,
		learningMinDuration: minDur,
		learningMaxDuration: maxDur,
		now:                 nowFn,
	}

	if err := writeRtpConnField(addr, wrapper); err != nil {
		return err
	}

	var localAddrStr string
	if la := cur.LocalAddr(); la != nil {
		localAddrStr = la.String()
	}
	wrapper.logf("armed: sdp=%s pts=%v trunk=%s call=%s sessLaddr=%s connLocalAddr=%s sessRaddr=%s",
		sdpRaddr.String(), opts.ExpectedPTs, opts.TrunkID, opts.CallID,
		sess.Laddr.String(), localAddrStr, sess.Raddr.String())

	verifyConn, _, verifyErr := readRtpConnField(sess)
	if verifyErr != nil {
		wrapper.logf("post-swap verify: ERROR re-reading field: %v", verifyErr)
	} else if verifyConn == wrapper {
		wrapper.logf("post-swap verify: OK wrapper installed (type=%T) innerType=%T", verifyConn, cur)
	} else {
		wrapper.logf("post-swap verify: FAILED expected *latchingPacketConn got %T addr=%p wrapper=%p",
			verifyConn, verifyConn, wrapper)
	}

	go wrapper.diagWatchdog(60 * time.Second)

	if uc, ok := cur.(*net.UDPConn); ok {
		if raw, err := uc.SyscallConn(); err == nil {
			var fdNum uintptr
			var rcvbuf int
			_ = raw.Control(func(fd uintptr) {
				fdNum = fd

				_ = syscallSetRcvBuf(int(fd), 4*1024*1024)
				rcvbuf = syscallGetRcvBuf(int(fd))
			})
			wrapper.logf("socket-info: fd=%d rcvbuf=%d trunk=%s call=%s",
				fdNum, rcvbuf, opts.TrunkID, opts.CallID)
		}
	}
	return nil
}

func (l *latchingPacketConn) diagWatchdog(total time.Duration) {
	deadline := time.Now().Add(total)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	// TODO: validate if for range on ticker.C channel, is the equivalent for the for{select{case <-ticker.C}} pattern, and if it will exit when the function returns.
	for range ticker.C {
		if l.diagFirstLogged.Load() {
			return
		}
		if time.Now().After(deadline) {
			l.logf("watchdog: giving up after %s with NO inbound — reads=%d writes=%d trunk=%s call=%s "+
				"[likely host firewall blocking UDP — see docs/deployment/SIP_RTP_FIREWALL.md]",
				total, l.diagReads.Load(), l.diagWrites.Load(), l.trunkID, l.callID)
			return
		}
		l.logf("watchdog: NO inbound yet reads=%d writes=%d redirectInstalled=%v trunk=%s call=%s",
			l.diagReads.Load(), l.diagWrites.Load(), l.redirectAddr.Load() != nil,
			l.trunkID, l.callID)
	}
}

const rtpConnFieldName = "rtpConn"

func readRtpConnField(sess *media.MediaSession) (net.PacketConn, unsafe.Pointer, error) {
	v := reflect.ValueOf(sess).Elem()
	f := v.FieldByName(rtpConnFieldName)
	if !f.IsValid() {
		return nil, nil, ErrLatchUnavailable
	}
	pcType := reflect.TypeOf((*net.PacketConn)(nil)).Elem()
	if f.Type() != pcType {
		return nil, nil, fmt.Errorf("%w: rtpConn field type is %s (expected net.PacketConn)",
			ErrLatchUnavailable, f.Type().String())
	}
	//nolint:gosec // unsafe access to a known unexported field of a pinned

	addr := unsafe.Pointer(f.UnsafeAddr())
	current := *(*net.PacketConn)(addr)
	return current, addr, nil
}

func writeRtpConnField(addr unsafe.Pointer, newConn net.PacketConn) error {
	if addr == nil {
		return ErrLatchUnavailable
	}
	*(*net.PacketConn)(addr) = newConn
	return nil
}

func PayloadTypesFromCodecs(codecs []media.Codec) []uint8 {
	seen := make(map[uint8]struct{}, len(codecs)+3)
	out := make([]uint8, 0, len(codecs)+3)
	for _, c := range codecs {
		if _, ok := seen[c.PayloadType]; ok {
			continue
		}
		seen[c.PayloadType] = struct{}{}
		out = append(out, c.PayloadType)
	}
	for _, pt := range [...]uint8{0, 8, 101} {
		if _, ok := seen[pt]; !ok {
			seen[pt] = struct{}{}
			out = append(out, pt)
		}
	}
	return out
}

var _ net.PacketConn = (*latchingPacketConn)(nil)
