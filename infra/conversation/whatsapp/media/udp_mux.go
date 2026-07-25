package media

import (
	"fmt"
	"net"
	"sync/atomic"

	"github.com/pion/ice/v4"
	"github.com/pion/logging"
	"github.com/pion/webrtc/v4"
)

var sharedUDPMux atomic.Pointer[muxHolder]

type muxHolder struct {
	mux  ice.UDPMux
	port int
}

func EnableSharedUDPMux(port int) (int, error) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: port})
	if err != nil {
		return 0, fmt.Errorf("whatsapp media udp mux listen :%d: %w", port, err)
	}
	bound := conn.LocalAddr().(*net.UDPAddr).Port
	mux := webrtc.NewICEUDPMux(logging.NewDefaultLoggerFactory().NewLogger("wa-udp-mux"), conn)
	sharedUDPMux.Store(&muxHolder{mux: mux, port: bound})
	return bound, nil
}

func loadSharedUDPMux() (ice.UDPMux, bool) {
	if h := sharedUDPMux.Load(); h != nil && h.mux != nil {
		return h.mux, true
	}
	return nil, false
}
