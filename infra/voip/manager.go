package voipinfra

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/emiago/diago/media"

	"vozko/domain/calls"
	"vozko/domain/sip_trunk"
)

const (
	registerRetryDelay = 5 * time.Second
	unregisterTimeout  = 5 * time.Second
	hangupTimeout      = 5 * time.Second
	publicIPTimeout    = 10 * time.Second
	stunTimeout        = 3 * time.Second
	portReleaseTimeout = 2 * time.Second
)

type TrunkManagerConfig struct {
	SIPBindHost     string
	SIPPortStart    int
	SIPPortCount    int
	RTPPortStart    int
	RTPPortEnd      int
	RegisterExpiry  time.Duration
	DialTimeout     time.Duration
	MediaTimeout    time.Duration
	MaxCallDuration time.Duration
	WatchInterval   time.Duration
	PublicAddress   string
	STUNServers     []string
	UserAgent       string
	Debug           bool
	CallMetrics     calls.CallMetricsRecorder

	AllowPrivateHosts bool
}

func (c TrunkManagerConfig) validate() error {
	switch {
	case c.SIPPortStart < 1024 || c.SIPPortStart > 65535:
		return fmt.Errorf("SIP port start %d outside [1024, 65535]", c.SIPPortStart)
	case c.SIPPortCount < 1 || c.SIPPortStart+c.SIPPortCount-1 > 65535:
		return fmt.Errorf("SIP port range %d+%d exceeds 65535", c.SIPPortStart, c.SIPPortCount)
	case c.RTPPortStart < 1024 || c.RTPPortEnd > 65535 || c.RTPPortEnd <= c.RTPPortStart:
		return fmt.Errorf("RTP port range %d-%d invalid", c.RTPPortStart, c.RTPPortEnd)
	case c.SIPPortStart <= c.RTPPortEnd && c.RTPPortStart <= c.SIPPortStart+c.SIPPortCount-1:
		return fmt.Errorf("SIP ports %d+%d overlap RTP ports %d-%d", c.SIPPortStart, c.SIPPortCount, c.RTPPortStart, c.RTPPortEnd)
	case c.RegisterExpiry <= 0 || c.DialTimeout <= 0 || c.MediaTimeout <= 0 || c.MaxCallDuration <= 0 || c.WatchInterval <= 0:
		return errors.New("register expiry, dial timeout, media timeout, max call duration and watch interval must be positive")
	case c.UserAgent == "":
		return errors.New("user agent is required")
	}
	return nil
}

type SIPTrunkManager struct {
	cfg      TrunkManagerConfig
	store    sip_trunk.EngineStore
	log      *log.Logger
	ports    *portAllocator
	publicIP *publicIPResolver
	limits   callLimits

	lifecycle sync.Mutex

	mu         sync.RWMutex
	ctx        context.Context
	cancel     context.CancelFunc
	running    bool
	trunks     map[string]*trunkConnection
	background sync.WaitGroup

	inboundMu      sync.RWMutex
	inboundHandler sip_trunk.InboundInviteHandler
}

var _ sip_trunk.Engine = (*SIPTrunkManager)(nil)

func NewSIPTrunkManager(cfg TrunkManagerConfig, store sip_trunk.EngineStore) (*SIPTrunkManager, error) {
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("sip trunk manager config: %w", err)
	}
	if cfg.SIPBindHost == "" {
		cfg.SIPBindHost = "0.0.0.0"
	}
	logger := log.New(log.Writer(), "sip-trunk-manager ", log.LstdFlags)
	if cfg.Debug {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))
		logger.Printf("SIP debug logging enabled: raw SIP messages, including auth headers, are logged")
	}
	media.RTPPortStart = cfg.RTPPortStart
	media.RTPPortEnd = cfg.RTPPortEnd
	return &SIPTrunkManager{
		cfg:      cfg,
		store:    store,
		log:      logger,
		ports:    newPortAllocator(cfg.SIPBindHost, cfg.SIPPortStart, cfg.SIPPortCount),
		publicIP: newPublicIPResolver(cfg.PublicAddress, cfg.STUNServers),
		limits:   callLimits{MediaTimeout: cfg.MediaTimeout, MaxDuration: cfg.MaxCallDuration, CheckInterval: cfg.WatchInterval},
		trunks:   make(map[string]*trunkConnection),
	}, nil
}

func (m *SIPTrunkManager) SetInboundInviteHandler(handler sip_trunk.InboundInviteHandler) {
	m.inboundMu.Lock()
	m.inboundHandler = handler
	m.inboundMu.Unlock()
}

func (m *SIPTrunkManager) currentInboundHandler() sip_trunk.InboundInviteHandler {
	m.inboundMu.RLock()
	defer m.inboundMu.RUnlock()
	return m.inboundHandler
}

func (m *SIPTrunkManager) defaults() trunkDefaults {
	return trunkDefaults{
		RegisterExpiry:     m.cfg.RegisterExpiry,
		RegisterRetryDelay: registerRetryDelay,
		UnregisterTimeout:  unregisterTimeout,
		UserAgent:          m.cfg.UserAgent,
		DialTimeout:        m.cfg.DialTimeout,
		STUNServers:        m.cfg.STUNServers,
	}
}

func (m *SIPTrunkManager) Start(ctx context.Context) error {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return errors.New("sip trunk manager already started")
	}
	m.ctx, m.cancel = context.WithCancel(ctx)
	m.running = true
	m.mu.Unlock()

	m.log.Printf("started: SIP ports %d-%d, RTP ports %d-%d", m.cfg.SIPPortStart, m.cfg.SIPPortStart+m.cfg.SIPPortCount-1, m.cfg.RTPPortStart, m.cfg.RTPPortEnd)
	m.background.Add(1)
	go func() {
		defer m.background.Done()
		m.registerEnabledTrunks()
	}()
	return nil
}

func (m *SIPTrunkManager) registerEnabledTrunks() {
	trunks, err := m.store.FindEnabled(m.ctx)
	if err != nil {
		m.log.Printf("loading enabled trunks failed: %v", err)
		return
	}
	m.log.Printf("registering %d enabled trunk(s)", len(trunks))
	for _, trunk := range trunks {
		if m.stopping() {
			return
		}
		if err := m.RegisterTrunk(trunk); err != nil {
			m.log.Printf("trunk %s: registration at startup failed: %v", trunk.ID, err)
		}
	}
}

func (m *SIPTrunkManager) stopping() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return !m.running
}

func (m *SIPTrunkManager) Stop() error {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return nil
	}
	m.running = false
	m.mu.Unlock()

	m.lifecycle.Lock()
	m.mu.Lock()
	connections := make([]*trunkConnection, 0, len(m.trunks))
	for _, conn := range m.trunks {
		connections = append(connections, conn)
	}
	m.trunks = make(map[string]*trunkConnection)
	m.mu.Unlock()

	var wg sync.WaitGroup
	for _, conn := range connections {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.teardown(conn)
		}()
	}
	wg.Wait()
	m.lifecycle.Unlock()

	m.cancel()
	m.background.Wait()
	m.log.Printf("stopped")
	return nil
}

func (m *SIPTrunkManager) RegisterTrunk(trunk *sip_trunk.SIPTrunk) error {
	if trunk == nil {
		return sip_trunk.ErrTrunkNotFound
	}
	m.lifecycle.Lock()
	defer m.lifecycle.Unlock()
	return m.registerLocked(trunk)
}

func (m *SIPTrunkManager) registerLocked(trunk *sip_trunk.SIPTrunk) error {
	m.mu.RLock()
	running := m.running
	_, exists := m.trunks[trunk.ID]
	m.mu.RUnlock()
	if !running {
		return sip_trunk.ErrEngineNotRunning
	}
	if exists {
		return nil
	}
	if !trunk.Enabled {
		return sip_trunk.ErrTrunkDisabled
	}

	snapshot := *trunk
	m.persistStatus(snapshot.ID, sip_trunk.RegistrationStatusRegistering, "")
	conn, err := m.connect(&snapshot)
	if err != nil {
		m.persistStatus(snapshot.ID, sip_trunk.RegistrationStatusFailed, err.Error())
		return err
	}
	m.mu.Lock()
	m.trunks[snapshot.ID] = conn
	m.mu.Unlock()
	conn.startRegistration()
	return nil
}

func (m *SIPTrunkManager) RefreshTrunk(trunk *sip_trunk.SIPTrunk) error {
	if trunk == nil {
		return sip_trunk.ErrTrunkNotFound
	}
	m.lifecycle.Lock()
	defer m.lifecycle.Unlock()
	m.removeLocked(trunk.ID)
	if !trunk.Enabled {
		m.persistStatus(trunk.ID, sip_trunk.RegistrationStatusUnregistered, "")
		return nil
	}
	return m.registerLocked(trunk)
}

func (m *SIPTrunkManager) UnregisterTrunk(trunkID string) error {
	m.lifecycle.Lock()
	defer m.lifecycle.Unlock()
	if m.removeLocked(trunkID) {
		m.persistStatus(trunkID, sip_trunk.RegistrationStatusUnregistered, "")
	}
	return nil
}

func (m *SIPTrunkManager) removeLocked(trunkID string) bool {
	m.mu.Lock()
	conn, exists := m.trunks[trunkID]
	delete(m.trunks, trunkID)
	m.mu.Unlock()
	if !exists {
		return false
	}
	m.teardown(conn)
	return true
}

func (m *SIPTrunkManager) teardown(conn *trunkConnection) {
	conn.markClosing()
	conn.hangupAll(hangupTimeout)
	conn.cancel()
	conn.workers.Wait()
	if conn.allocatedPort {
		waitPortReleased(m.cfg.SIPBindHost, conn.listenPort, portReleaseTimeout)
		m.ports.release(conn.listenPort)
	}
	m.log.Printf("trunk %s: connection closed", conn.trunk.ID)
}

func waitPortReleased(host string, port int, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for !udpPortFree(host, port) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
}

func (m *SIPTrunkManager) TrunkStatus(trunkID string) (sip_trunk.SIPTrunkStatusUpdate, bool) {
	conn, ok := m.connection(trunkID)
	if !ok {
		return sip_trunk.SIPTrunkStatusUpdate{}, false
	}
	return conn.statusUpdate(), true
}

func (m *SIPTrunkManager) ActiveCalls(trunkID string) []sip_trunk.ActiveCall {
	conn, ok := m.connection(trunkID)
	if !ok {
		return nil
	}
	return conn.activeCalls()
}

func (m *SIPTrunkManager) connection(trunkID string) (*trunkConnection, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	conn, ok := m.trunks[trunkID]
	return conn, ok
}

func (m *SIPTrunkManager) persistStatus(trunkID string, status sip_trunk.RegistrationStatus, lastError string) {
	if err := m.store.UpdateStatus(context.Background(), trunkID, status, lastError); err != nil {
		m.log.Printf("trunk %s: persisting status %s failed: %v", trunkID, status, err)
	}
}
