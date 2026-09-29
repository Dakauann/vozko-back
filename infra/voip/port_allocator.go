package voipinfra

import (
	"fmt"
	"net"
	"sync"
)

type portAllocator struct {
	mu       sync.Mutex
	bindHost string
	basePort int
	count    int
	used     map[int]struct{}
}

func newPortAllocator(bindHost string, basePort, count int) *portAllocator {
	return &portAllocator{bindHost: bindHost, basePort: basePort, count: count, used: make(map[int]struct{})}
}

func (p *portAllocator) allocate() (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for port := p.basePort; port < p.basePort+p.count; port++ {
		if _, taken := p.used[port]; taken {
			continue
		}
		if !udpPortFree(p.bindHost, port) {
			continue
		}
		p.used[port] = struct{}{}
		return port, nil
	}
	return 0, fmt.Errorf("no free SIP port in %d-%d", p.basePort, p.basePort+p.count-1)
}

func (p *portAllocator) release(port int) {
	p.mu.Lock()
	delete(p.used, port)
	p.mu.Unlock()
}

func udpPortFree(host string, port int) bool {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP(host), Port: port})
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
