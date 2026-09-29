package voipinfra

import (
	"net"
	"sync"

	"github.com/emiago/diago/media"
)

const rtpHeaderSize = 12

type latchingConn struct {
	net.PacketConn
	expected map[uint8]struct{}

	mu      sync.RWMutex
	latched *net.UDPAddr
}

func newLatchingConn(conn net.PacketConn, expectedPayloadTypes []uint8) *latchingConn {
	expected := make(map[uint8]struct{}, len(expectedPayloadTypes))
	for _, pt := range expectedPayloadTypes {
		expected[pt] = struct{}{}
	}
	return &latchingConn{PacketConn: conn, expected: expected}
}

func (c *latchingConn) ReadFrom(b []byte) (int, net.Addr, error) {
	for {
		n, from, err := c.PacketConn.ReadFrom(b)
		if err != nil {
			return n, from, err
		}
		source, ok := from.(*net.UDPAddr)
		if !ok {
			continue
		}
		latched := c.current()
		if latched == nil {
			if !c.isExpectedRTP(b[:n]) {
				continue
			}
			c.latch(source)
			return n, from, nil
		}
		if sameUDPAddr(latched, source) {
			return n, from, nil
		}
	}
}

func (c *latchingConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	if latched := c.current(); latched != nil {
		return c.PacketConn.WriteTo(b, latched)
	}
	return c.PacketConn.WriteTo(b, addr)
}

func (c *latchingConn) Reset() {
	c.mu.Lock()
	c.latched = nil
	c.mu.Unlock()
}

func (c *latchingConn) current() *net.UDPAddr {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.latched
}

func (c *latchingConn) latch(source *net.UDPAddr) {
	copied := *source
	c.mu.Lock()
	c.latched = &copied
	c.mu.Unlock()
}

func (c *latchingConn) isExpectedRTP(packet []byte) bool {
	if len(packet) < rtpHeaderSize || packet[0]>>6 != 2 {
		return false
	}
	_, ok := c.expected[packet[1]&0x7f]
	return ok
}

func sameUDPAddr(a, b *net.UDPAddr) bool {
	return a.Port == b.Port && a.IP.Equal(b.IP)
}

func payloadTypesFromCodecs(codecs []media.Codec) []uint8 {
	types := make([]uint8, 0, len(codecs))
	for _, codec := range codecs {
		types = append(types, codec.PayloadType)
	}
	return types
}

func attachLatch(session *media.MediaSession) (*latchingConn, error) {
	field, err := sessionField(session, "rtpConn")
	if err != nil {
		return nil, err
	}
	if field.IsNil() {
		return nil, errMediaSessionNotInitialised
	}
	current := field.Interface().(net.PacketConn)
	if existing, ok := current.(*latchingConn); ok {
		return existing, nil
	}
	latch := newLatchingConn(current, payloadTypesFromCodecs(session.Codecs))
	field.Set(packetConnValue(net.PacketConn(latch)))
	return latch, nil
}
