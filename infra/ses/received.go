package ses

import (
	"bufio"
	"bytes"
	"net/netip"
	"net/textproto"
	"strings"

	"vozko/domain/emailsending"
)

const (
	maxHeaderScanBytes = 256 << 10
	sesReceiverPrefix  = "inbound-smtp."
	sesReceiverSuffix  = ".amazonaws.com"
)

type ReceivedHeaders struct{}

var _ emailsending.ConnectingIPReader = ReceivedHeaders{}

func (ReceivedHeaders) ConnectingIP(raw []byte) (netip.Addr, bool) {
	header, _ := textproto.NewReader(bufio.NewReader(bytes.NewReader(raw[:min(len(raw), maxHeaderScanBytes)]))).ReadMIMEHeader()
	received := header.Values("Received")
	if len(received) == 0 {
		return netip.Addr{}, false
	}
	return sesConnectingIP(received[0])
}

func sesConnectingIP(received string) (netip.Addr, bool) {
	from, rest, ok := strings.Cut(received, " by ")
	if !ok || !strings.HasPrefix(strings.TrimSpace(from), "from ") {
		return netip.Addr{}, false
	}
	receiver, _, _ := strings.Cut(strings.TrimSpace(rest), " ")
	receiver = strings.ToLower(receiver)
	if !strings.HasPrefix(receiver, sesReceiverPrefix) || !strings.HasSuffix(receiver, sesReceiverSuffix) {
		return netip.Addr{}, false
	}
	open := strings.LastIndexByte(from, '[')
	end := strings.LastIndexByte(from, ']')
	if open < 0 || end < open {
		return netip.Addr{}, false
	}
	literal := strings.TrimPrefix(from[open+1:end], "IPv6:")
	ip, err := netip.ParseAddr(literal)
	if err != nil {
		return netip.Addr{}, false
	}
	return ip.Unmap(), true
}
