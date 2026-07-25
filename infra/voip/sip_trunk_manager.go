package voipinfra

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/emiago/diago"
	"github.com/emiago/diago/media"
	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
	"github.com/pion/rtp"

	"vozko/brand"
	"vozko/domain/calls"
	"vozko/domain/metrics"
	"vozko/domain/sip_trunk"
	"vozko/domain/voip"
	"vozko/infra/voip/audio"
)

type trunkConnection struct {
	trunk      *sip_trunk.SIPTrunk
	ua         *sipgo.UserAgent
	client     *diago.Diago
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	mu         sync.RWMutex
	listenPort int
	registered chan struct{}
	regOnce    sync.Once

	runtime trunkRuntimeConfig

	dialogs        map[string]*diago.DialogClientSession
	inboundDialogs map[string]*diago.DialogServerSession
	dialogsMu      sync.RWMutex
}

type portAllocator struct {
	mu        sync.Mutex
	basePort  int
	usedPorts map[int]bool
	maxPorts  int
	bindHost  string
}

func newPortAllocator(basePort, maxPorts int) *portAllocator {
	return &portAllocator{
		basePort:  basePort,
		usedPorts: make(map[int]bool),
		maxPorts:  maxPorts,
		bindHost:  "0.0.0.0",
	}
}

func isPortAvailable(host string, port int) bool {
	addr := &net.UDPAddr{IP: net.ParseIP(host), Port: port}
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func (p *portAllocator) allocate() (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i := 0; i < p.maxPorts; i++ {
		port := p.basePort + i
		if p.usedPorts[port] {
			continue
		}

		if !isPortAvailable(p.bindHost, port) {
			p.usedPorts[port] = true
			continue
		}
		p.usedPorts[port] = true
		return port, nil
	}
	return 0, fmt.Errorf("no available ports in range %d-%d (all %d ports occupied)", p.basePort, p.basePort+p.maxPorts-1, p.maxPorts)
}

func (p *portAllocator) release(port int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.usedPorts, port)
}

type SIPTrunkManager struct {
	repo                sip_trunk.Repository
	metrics             metrics.MetricsService
	callMetrics         calls.CallMetricsRecorder
	log                 *log.Logger
	sipPortAllocator    *portAllocator
	registerInterval    time.Duration
	registrationTimeout time.Duration
	trunks              map[string]*trunkConnection
	mu                  sync.RWMutex
	ctx                 context.Context
	cancel              context.CancelFunc
	statusCallback      func(update sip_trunk.SIPTrunkStatusUpdate)
	started             bool

	mediaSessionRecorder voip.MediaSessionRecorder

	ownership *sip_trunk.TrunkOwnershipManager

	replicas sip_trunk.LiveReplicasSource

	bgWG sync.WaitGroup

	registerFn   func(*sip_trunk.SIPTrunk) error
	unregisterFn func(string) error

	inboundMu      sync.RWMutex
	inboundHandler sip_trunk.InboundInviteHandler
}

type TrunkManagerConfig struct {
	SIPPortStart        int
	SIPPortCount        int
	RTPPortStart        int
	RTPPortEnd          int
	RegisterInterval    time.Duration
	RegistrationTimeout time.Duration
	Metrics             metrics.MetricsService
	CallMetrics         calls.CallMetricsRecorder

	Ownership *sip_trunk.TrunkOwnershipManager

	Replicas sip_trunk.LiveReplicasSource
}

func NewSIPTrunkManager(cfg TrunkManagerConfig, repo sip_trunk.Repository) (*SIPTrunkManager, error) {
	logger := log.New(log.Writer(), "sip-trunk-manager ", log.LstdFlags)

	// SIP_DEBUG=1 raises slog to Debug, which makes sipgo log every raw SIP
	// message (requests + responses with all headers — e.g. the exact 401/407
	// challenge a provider returns). Use it to diagnose auth failures like
	// "No WWW-Authenticate header present" and tell our-code vs provider apart.
	// Off by default: it's verbose and SIP auth headers are sensitive.
	if os.Getenv("SIP_DEBUG") == "1" {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})))
		logger.Printf("SIP_DEBUG=1: raw SIP message logging enabled (slog Debug)")
	}

	registerInterval := cfg.RegisterInterval
	if registerInterval == 0 {
		registerInterval = 25 * time.Second
	}

	registrationTimeout := cfg.RegistrationTimeout
	if registrationTimeout == 0 {
		registrationTimeout = 30 * time.Second
	}

	sipPortStart := cfg.SIPPortStart
	if sipPortStart == 0 {
		sipPortStart = 5063
	}

	sipPortCount := cfg.SIPPortCount
	if sipPortCount == 0 {
		sipPortCount = 100
	}

	rtpPortStart := cfg.RTPPortStart
	if rtpPortStart == 0 {
		rtpPortStart = 20000
	}

	rtpPortEnd := cfg.RTPPortEnd
	if rtpPortEnd == 0 {
		rtpPortEnd = 59999
	}

	if sipPortStart < 1024 || sipPortStart > 65535 {
		return nil, fmt.Errorf("SIPPortStart %d out of valid range [1024, 65535]", sipPortStart)
	}
	if sipPortStart+sipPortCount-1 > 65535 {
		return nil, fmt.Errorf("SIP port range %d-%d exceeds max port 65535", sipPortStart, sipPortStart+sipPortCount-1)
	}
	if rtpPortStart < 1024 || rtpPortStart > 65535 {
		return nil, fmt.Errorf("RTPPortStart %d out of valid range [1024, 65535]", rtpPortStart)
	}
	if rtpPortEnd > 65535 {
		return nil, fmt.Errorf("RTPPortEnd %d exceeds max port 65535", rtpPortEnd)
	}
	if rtpPortEnd <= rtpPortStart {
		return nil, fmt.Errorf("RTPPortEnd %d must be greater than RTPPortStart %d", rtpPortEnd, rtpPortStart)
	}

	// Capacity is RTP/RTCP PAIRS, not ports. A plain call leg uses 1 pair; a branch-
	// bridged transfer anchors BOTH legs (caller + phone) so it uses 2 pairs. The
	// RTP-vs-ephemeral-range safety check is enforced (fatally) at boot by the
	// container's validatePortLayout, so it is not repeated here.
	rtpPairs := (rtpPortEnd - rtpPortStart) / 2
	logger.Printf("Port config: SIP %d-%d (%d trunks), RTP %d-%d (%d RTP/RTCP pairs = %d single-leg calls, ~%d branch-bridged transfers)",
		sipPortStart, sipPortStart+sipPortCount-1, sipPortCount,
		rtpPortStart, rtpPortEnd, rtpPairs, rtpPairs, rtpPairs/2)

	media.RTPPortStart = rtpPortStart
	media.RTPPortEnd = rtpPortEnd

	if cfg.Ownership == nil {
		return nil, fmt.Errorf("TrunkManagerConfig.Ownership is required (multi-replica coordination)")
	}

	m := &SIPTrunkManager{
		repo:                repo,
		metrics:             cfg.Metrics,
		callMetrics:         cfg.CallMetrics,
		log:                 logger,
		sipPortAllocator:    newPortAllocator(sipPortStart, sipPortCount),
		registerInterval:    registerInterval,
		registrationTimeout: registrationTimeout,
		trunks:              make(map[string]*trunkConnection),
		ownership:           cfg.Ownership,
		replicas:            cfg.Replicas,
	}
	m.registerFn = m.registerTrunkInternal
	m.unregisterFn = m.unregisterTrunkInternal
	return m, nil
}

func (m *SIPTrunkManager) SetMediaSessionRecorder(r voip.MediaSessionRecorder) {
	m.mediaSessionRecorder = r
}

func (m *SIPTrunkManager) defaults() trunkDefaults {
	return trunkDefaults{
		RegisterExpiry:     m.registerInterval,
		RegisterRetryDelay: 5 * time.Second,
		UnregisterTimeout:  5 * time.Second,
		UserAgentFormat:    brand.Active().Name + "Trunk/%s",
		DialTimeout:        60 * time.Second,
		MediaWait:          3 * time.Second,
		STUNTimeout:        stunQueryTimeout,
		PublicIPTimeout:    15 * time.Second,
		STUNServers:        nil,
	}
}

func (m *SIPTrunkManager) Start(ctx context.Context) error {
	if m.started {
		return fmt.Errorf("trunk manager already started")
	}

	m.ctx, m.cancel = context.WithCancel(ctx)
	m.started = true

	m.log.Printf("SIP Trunk Manager started (replica=%s, RTP ports %d-%d)",
		m.ownership.ReplicaID(), media.RTPPortStart, media.RTPPortEnd)

	m.ownership.SetCallbacks(sip_trunk.OwnershipCallbacks{
		OnAcquired: m.handleOwnershipAcquired,
		OnLost:     m.handleOwnershipLost,
	})

	m.bgWG.Add(1)
	go func() {
		defer m.bgWG.Done()
		m.ownership.RunHeartbeat(m.ctx)
	}()

	m.bgWG.Add(1)
	go func() {
		defer m.bgWG.Done()
		if m.replicas != nil {
			m.ownership.RunReconciler(m.ctx, m.replicas, m.repo.FindEnabled)
			return
		}
		m.ownership.RunTakeover(m.ctx, m.repo.FindEnabled)
	}()

	m.bgWG.Add(1)
	go func() {
		defer m.bgWG.Done()
		if m.replicas != nil {

			return
		}
		trunks, err := m.repo.FindEnabled()
		if err != nil {
			m.log.Printf("Initial FindEnabled failed: %v", err)
			return
		}
		m.log.Printf("Initial trunk acquisition pass: %d enabled trunks", len(trunks))
		for _, t := range trunks {
			if t == nil {
				continue
			}
			select {
			case <-m.ctx.Done():
				return
			default:
			}
			if _, err := m.ownership.TryAcquire(t.ID); err != nil {
				m.log.Printf("Initial TryAcquire trunk %s failed: %v", t.ID, err)
			}
		}
	}()

	return nil
}

func (m *SIPTrunkManager) Stop() error {
	if !m.started {
		return nil
	}

	m.cancel()
	m.bgWG.Wait()

	for _, id := range m.ownership.OwnedSnapshot() {
		if err := m.ownership.Release(id); err != nil {
			m.log.Printf("Trunk %s: Release on shutdown failed: %v", id, err)
		}
	}

	m.mu.Lock()
	for id, conn := range m.trunks {
		m.log.Printf("Stopping trunk %s", id)
		conn.cancel()
		conn.wg.Wait()
		if conn.listenPort > 0 {
			m.sipPortAllocator.release(conn.listenPort)
		}
	}
	m.trunks = make(map[string]*trunkConnection)
	m.mu.Unlock()

	m.started = false
	return nil
}

func (m *SIPTrunkManager) handleOwnershipAcquired(trunkID string) {
	trunk, err := m.repo.FindByID(trunkID)
	if err != nil {
		m.log.Printf("Trunk %s: ownership acquired but FindByID failed: %v — releasing", trunkID, err)
		_ = m.ownership.Release(trunkID)
		return
	}
	if trunk == nil || !trunk.Enabled {
		m.log.Printf("Trunk %s: ownership acquired but trunk is disabled/missing — releasing", trunkID)
		_ = m.ownership.Release(trunkID)
		return
	}
	if err := m.registerFn(trunk); err != nil {
		m.log.Printf("Trunk %s: register after ownership acquire failed: %v — releasing", trunkID, err)
		_ = m.ownership.Release(trunkID)
	}
}

func (m *SIPTrunkManager) handleOwnershipLost(trunkID string) {
	m.log.Printf("Trunk %s: ownership lost — tearing down local connection", trunkID)
	if err := m.unregisterFn(trunkID); err != nil && !errors.Is(err, sip_trunk.ErrTrunkNotFound) {
		m.log.Printf("Trunk %s: unregister after ownership loss failed: %v", trunkID, err)
	}
}

func (m *SIPTrunkManager) RegisterTrunk(trunk *sip_trunk.SIPTrunk) error {
	if trunk == nil {
		return fmt.Errorf("RegisterTrunk: trunk is nil")
	}

	acquired, err := m.ownership.TryAcquire(trunk.ID)
	if err != nil {
		return fmt.Errorf("acquire ownership of trunk %s: %w", trunk.ID, err)
	}
	if !acquired {
		return sip_trunk.ErrTrunkNotOwnedHere
	}

	if err := m.registerFn(trunk); err != nil {
		_ = m.ownership.Release(trunk.ID)
		return err
	}
	return nil
}

func (m *SIPTrunkManager) registerTrunkInternal(trunk *sip_trunk.SIPTrunk) error {
	m.mu.Lock()
	if _, exists := m.trunks[trunk.ID]; exists {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	m.log.Printf("Trunk %s: Creating connection...", trunk.ID)

	conn, err := m.createTrunkConnection(trunk)
	if err != nil {
		m.updateStatus(trunk.ID, sip_trunk.RegistrationStatusFailed, err.Error())
		return err
	}

	m.mu.Lock()
	m.trunks[trunk.ID] = conn
	m.mu.Unlock()

	m.log.Printf("Trunk %s connection created, starting registration", trunk.ID)

	return nil
}

func (m *SIPTrunkManager) RegisterTrunkAndWait(trunk *sip_trunk.SIPTrunk, timeout time.Duration) error {
	if err := m.RegisterTrunk(trunk); err != nil {
		return err
	}
	return m.WaitForRegistration(trunk.ID, timeout)
}

func (m *SIPTrunkManager) WaitForRegistration(trunkID string, timeout time.Duration) error {
	if timeout == 0 {
		timeout = m.registrationTimeout
	}

	m.mu.RLock()
	conn, exists := m.trunks[trunkID]
	m.mu.RUnlock()

	if !exists {
		return sip_trunk.ErrTrunkNotFound
	}

	if conn.trunk.RegistrationStatus == sip_trunk.RegistrationStatusRegistered {
		return nil
	}

	select {
	case <-conn.registered:
		if conn.trunk.RegistrationStatus == sip_trunk.RegistrationStatusRegistered {
			return nil
		}
		return fmt.Errorf("trunk %s registration failed: %s", trunkID, conn.trunk.LastError)
	case <-time.After(timeout):
		return fmt.Errorf("trunk %s registration timed out after %v", trunkID, timeout)
	case <-m.ctx.Done():
		return m.ctx.Err()
	}
}

func (m *SIPTrunkManager) IsRegistered(trunkID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	conn, exists := m.trunks[trunkID]
	if !exists {
		return false
	}
	return conn.trunk.RegistrationStatus == sip_trunk.RegistrationStatusRegistered
}

func (m *SIPTrunkManager) GetTrunkClient(trunkID string) (*diago.Diago, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	conn, exists := m.trunks[trunkID]
	if !exists {
		return nil, sip_trunk.ErrTrunkNotFound
	}

	if conn.trunk.RegistrationStatus != sip_trunk.RegistrationStatusRegistered {
		return nil, sip_trunk.ErrTrunkNotRegistered
	}

	return conn.client, nil
}

type mediaSessionAdapter struct {
	session   *media.MediaSession
	readCount atomic.Uint64
	diagLog   *log.Logger
	diagID    string

	dtmfMu            sync.RWMutex
	dtmfHandler       voip.DTMFHandler
	dtmfPTResolved    bool
	dtmfPT            uint8
	dtmfLastTimestamp uint32
	dtmfLastFired     bool
}

const dtmfEventChars = "0123456789*#ABCD"

func (m *mediaSessionAdapter) resolveDTMFPayloadType() uint8 {
	if m.dtmfPTResolved {
		return m.dtmfPT
	}
	for _, c := range m.session.Codecs {
		if c.Name == "telephone-event" {
			m.dtmfPT = uint8(c.PayloadType)
			break
		}
	}
	m.dtmfPTResolved = true
	return m.dtmfPT
}

func (m *mediaSessionAdapter) handleDTMFPacket(p *rtp.Packet) {
	if len(p.Payload) < 4 {
		return
	}
	m.dtmfMu.RLock()
	handler := m.dtmfHandler
	m.dtmfMu.RUnlock()
	if handler == nil {
		return
	}

	event := p.Payload[0]
	endOfEvent := p.Payload[1]&0x80 != 0

	if p.Timestamp != m.dtmfLastTimestamp {
		m.dtmfLastTimestamp = p.Timestamp
		m.dtmfLastFired = false
	}
	if !endOfEvent || m.dtmfLastFired || int(event) >= len(dtmfEventChars) {
		return
	}
	m.dtmfLastFired = true
	handler(rune(dtmfEventChars[event]))
}

func (m *mediaSessionAdapter) OnDTMF(handler voip.DTMFHandler) {
	m.dtmfMu.Lock()
	m.dtmfHandler = handler
	m.dtmfMu.Unlock()
}

func (m *mediaSessionAdapter) ReadRTP(buf []byte, packet interface{}) (int, error) {
	rtpPacket, ok := packet.(*rtp.Packet)
	if !ok {
		return 0, fmt.Errorf("packet must be *rtp.Packet")
	}

	for {
		c := m.readCount.Add(1)
		if c == 1 && m.diagLog != nil {
			m.diagLog.Printf("[adapter] %s first ReadRTP call entered", m.diagID)
		}
		n, err := m.session.ReadRTP(buf, rtpPacket)
		if c == 1 && m.diagLog != nil {
			m.diagLog.Printf("[adapter] %s first ReadRTP returned n=%d err=%v", m.diagID, n, err)
		}
		if err != nil {
			return n, err
		}
		if dtmfPT := m.resolveDTMFPayloadType(); dtmfPT != 0 && rtpPacket.PayloadType == dtmfPT {
			m.handleDTMFPacket(rtpPacket)
			continue
		}
		return n, nil
	}
}

func (m *mediaSessionAdapter) WriteRTP(packet interface{}) error {
	rtpPacket, ok := packet.(*rtp.Packet)
	if !ok {
		return fmt.Errorf("packet must be *rtp.Packet")
	}
	return m.session.WriteRTP(rtpPacket)
}

func (m *mediaSessionAdapter) LocalAddr() net.Addr {
	return &m.session.Laddr
}

func (m *mediaSessionAdapter) RemoteAddr() net.Addr {
	return &m.session.Raddr
}

func (m *mediaSessionAdapter) Close() error {
	return m.session.Close()
}

// NegotiatedCodec reports the call's negotiated voice codec — the first
// non-DTMF codec common to both ends — as the single source of truth for
// encoding/decoding. Mirrors the selection logic in buildMediaInfo.
func (m *mediaSessionAdapter) NegotiatedCodec() voip.CodecInfo {
	if m == nil || m.session == nil {
		return voip.CodecInfo{}
	}
	negotiated := m.session.CommonCodecs()
	if len(negotiated) == 0 {
		negotiated = m.session.Codecs
	}
	for _, c := range negotiated {
		if c.Name == "telephone-event" {
			continue
		}
		return voip.CodecInfo{
			Name:        c.Name,
			PayloadType: c.PayloadType,
			SampleRate:  c.SampleRate,
			Channels:    c.NumChannels,
			PtimeMs:     int(c.SampleDur.Milliseconds()),
		}
	}
	return voip.CodecInfo{}
}

func (m *mediaSessionAdapter) UnblockReaders() error {
	if m == nil || m.session == nil {
		return errors.New("media session adapter: nil session")
	}
	conn, _, err := readRtpConnField(m.session)
	if err != nil {
		return err
	}
	deadliner, ok := conn.(interface {
		SetReadDeadline(t time.Time) error
	})
	if !ok {
		return errors.New("media session adapter: inner conn does not support deadlines")
	}
	if m.diagLog != nil {
		m.diagLog.Printf("[media-handoff] %s UnblockReaders: setting past read deadline to wake the blocked RTP reader (surrender hand-off)", m.diagID)
	}
	if err := deadliner.SetReadDeadline(time.Now()); err != nil {
		if m.diagLog != nil {
			m.diagLog.Printf("[media-handoff] %s UnblockReaders: SetReadDeadline(now) failed: %v", m.diagID, err)
		}
		return err
	}

	time.Sleep(50 * time.Millisecond)

	if err := deadliner.SetReadDeadline(time.Time{}); err != nil {
		if m.diagLog != nil {
			m.diagLog.Printf("[media-handoff] %s UnblockReaders: clearing read deadline failed: %v", m.diagID, err)
		}
		return err
	}
	if m.diagLog != nil {
		m.diagLog.Printf("[media-handoff] %s UnblockReaders: read deadline cleared; reader resumes (buffer must stay alive for the human leg)", m.diagID)
	}
	return nil
}

func (m *SIPTrunkManager) Invite(ctx context.Context, trunkID string, input sip_trunk.TrunkInviteInput) (sip_trunk.TrunkCallSession, error) {

	if !m.ownership.IsOwner(trunkID) {
		return sip_trunk.TrunkCallSession{}, sip_trunk.ErrTrunkNotOwnedHere
	}

	m.mu.RLock()
	conn, exists := m.trunks[trunkID]
	m.mu.RUnlock()

	if !exists {
		return sip_trunk.TrunkCallSession{}, sip_trunk.ErrTrunkNotFound
	}

	if conn.trunk.RegistrationStatus != sip_trunk.RegistrationStatusRegistered {
		return sip_trunk.TrunkCallSession{}, sip_trunk.ErrTrunkNotRegistered
	}

	if conn.client == nil {
		return sip_trunk.TrunkCallSession{}, fmt.Errorf("trunk %s client not initialized", trunkID)
	}

	domain := conn.trunk.Domain
	if domain == "" {
		domain = conn.trunk.Host
	}

	dialedNumber := conn.runtime.ApplyDialPlan(input.PhoneNumber)
	if dialedNumber != input.PhoneNumber {
		m.log.Printf("Trunk %s: dial-plan transformed %s -> %s", trunkID, input.PhoneNumber, dialedNumber)
	}

	recipient := sip.Uri{
		User: dialedNumber,
		Host: domain,
	}

	dialCtx, dialCancel := context.WithTimeout(ctx, conn.runtime.DialTimeout)
	defer dialCancel()

	m.log.Printf("Trunk %s: Inviting %s via %s", trunkID, dialedNumber, domain)

	inviteOpts := diago.InviteOptions{
		Username: conn.runtime.AuthUsername,
		Password: conn.trunk.Password,
		Headers:  []sip.Header{},
	}
	// Set the From identity to the trunk's SIP account (username@domain), not the
	// default UA identity (<Brand>Trunk/<trunkID>@<our-ip>). Registration-based
	// providers (e.g. kvoip) only accept INVITEs from the registered AOR and
	// reject anything else with a 401 — which is why dialing failed while a
	// softphone using <username>@<domain> works. sipgo keeps a pre-set From
	// (it only builds a default when From is nil), so injecting it here wins.
	if conn.trunk.Username != "" {
		inviteOpts.Headers = append(inviteOpts.Headers, &sip.FromHeader{
			Address: sip.Uri{User: conn.trunk.Username, Host: domain},
			Params:  sip.NewParams(),
		})
	}
	if len(conn.runtime.ExtraInviteHeaders) > 0 {
		inviteOpts.Headers = append(inviteOpts.Headers, conn.runtime.ExtraInviteHeaders...)
	}

	sm := calls.NewCallStateMachine(m.callMetrics)
	sm.Dialing()

	dialog, err := conn.client.Invite(dialCtx, recipient, inviteOpts)
	if err != nil {
		sm.LeaveDialing()
		sm.Finished()
		return sip_trunk.TrunkCallSession{}, fmt.Errorf("invite failed: %w", err)
	}

	mediaSession := dialog.MediaSession()
	if mediaSession == nil {
		mediaWaitDeadline := time.Now().Add(conn.runtime.MediaWait)
		for mediaSession == nil && time.Now().Before(mediaWaitDeadline) {
			select {
			case <-dialCtx.Done():
				sm.LeaveDialing()
				sm.Finished()
				_ = dialog.Close()
				return sip_trunk.TrunkCallSession{}, fmt.Errorf("invite failed: context done while waiting for media session: %w", dialCtx.Err())
			case <-time.After(50 * time.Millisecond):
				mediaSession = dialog.MediaSession()
			}
		}
	}

	if mediaSession == nil {
		sm.LeaveDialing()
		sm.Finished()
		_ = dialog.Close()
		return sip_trunk.TrunkCallSession{}, fmt.Errorf("invite failed: no media session available")
	}

	mediaInfo := buildMediaInfo(mediaSession)
	localAddr := mediaSession.Laddr.String()
	remoteAddr := mediaSession.Raddr.String()

	if err := WrapMediaSessionForLatching(mediaSession, LatchOptions{
		ExpectedPTs: PayloadTypesFromCodecs(mediaSession.Codecs),
		TrunkID:     trunkID,
		CallID:      dialog.ID,
		Logger:      m.log,
	}); err != nil {
		m.log.Printf("Trunk %s call %s: RTP latching unavailable (%v); proceeding without it",
			trunkID, dialog.ID, err)
	}

	adapter := &mediaSessionAdapter{session: mediaSession, diagLog: m.log, diagID: dialog.ID}

	reorderBuf := audio.NewRTPReorderBuffer(adapter, audio.RTPReorderBufferOptions{
		Depth:   3,
		MaxWait: 60 * time.Millisecond,
		Logger:  m.log.Printf,
		CallID:  string(dialog.ID),
	})
	reorderBuf.Run(m.ctx)
	var wrappedMedia voip.MediaSession = reorderBuf
	if m.mediaSessionRecorder != nil {
		meta := voip.RecordingMeta{}
		if input.Recording != nil {
			meta = *input.Recording
		}
		wrappedMedia = m.mediaSessionRecorder(reorderBuf, string(dialog.ID), meta)
	}

	conn.dialogsMu.Lock()
	conn.dialogs[dialog.ID] = dialog
	conn.dialogsMu.Unlock()

	sm.Answered()

	dialog.OnClose(func() error {
		sm.LeaveOngoing()
		sm.Finished()
		conn.dialogsMu.Lock()
		delete(conn.dialogs, dialog.ID)
		conn.dialogsMu.Unlock()
		_ = reorderBuf.Close()
		_ = wrappedMedia.Close()
		return nil
	})

	session := sip_trunk.TrunkCallSession{
		ID:           dialog.ID,
		TrunkID:      trunkID,
		PhoneNumber:  input.PhoneNumber,
		Direction:    "outbound",
		StartedAt:    time.Now(),
		LocalAddr:    localAddr,
		RemoteAddr:   remoteAddr,
		MediaSession: wrappedMedia,
		Media:        mediaInfo,
	}

	return session, nil
}

func (m *SIPTrunkManager) Hangup(ctx context.Context, trunkID string, callID string) error {

	if !m.ownership.IsOwner(trunkID) {
		return sip_trunk.ErrTrunkNotOwnedHere
	}

	m.mu.RLock()
	conn, exists := m.trunks[trunkID]
	m.mu.RUnlock()

	if !exists {
		return sip_trunk.ErrTrunkNotFound
	}

	conn.dialogsMu.Lock()
	dialog, exists := conn.dialogs[callID]
	if exists {
		delete(conn.dialogs, callID)
	}
	inboundDialog, inboundExists := conn.inboundDialogs[callID]
	if inboundExists {
		delete(conn.inboundDialogs, callID)
	}
	conn.dialogsMu.Unlock()

	if !exists && !inboundExists {
		m.log.Printf("Trunk %s: dialog %s not found for hangup (may have already ended)", trunkID, callID)
		return nil
	}

	m.log.Printf("Trunk %s: Hanging up call %s", trunkID, callID)
	if inboundExists {
		if err := inboundDialog.Hangup(ctx); err != nil {
			m.log.Printf("Trunk %s: inbound hangup error for %s: %v", trunkID, callID, err)
			return err
		}
		return nil
	}

	if err := dialog.Hangup(ctx); err != nil {
		m.log.Printf("Trunk %s: hangup error for %s: %v", trunkID, callID, err)
		return err
	}

	return nil
}

func (m *SIPTrunkManager) UnregisterTrunk(trunkID string) error {
	owned := m.ownership.IsOwner(trunkID)

	if owned {

		return m.ownership.Release(trunkID)
	}

	return m.unregisterFn(trunkID)
}

func (m *SIPTrunkManager) unregisterTrunkInternal(trunkID string) error {
	m.mu.Lock()
	conn, exists := m.trunks[trunkID]
	if !exists {
		m.mu.Unlock()
		return sip_trunk.ErrTrunkNotFound
	}
	delete(m.trunks, trunkID)
	m.mu.Unlock()

	conn.cancel()
	conn.wg.Wait()

	time.Sleep(500 * time.Millisecond)

	if conn.listenPort > 0 {
		m.sipPortAllocator.release(conn.listenPort)
	}

	m.updateStatus(trunkID, sip_trunk.RegistrationStatusUnregistered, "")
	m.log.Printf("Trunk %s unregistered", trunkID)
	return nil
}

func (m *SIPTrunkManager) RefreshTrunk(trunk *sip_trunk.SIPTrunk) error {

	if err := m.UnregisterTrunk(trunk.ID); err != nil && err != sip_trunk.ErrTrunkNotFound {
		return err
	}

	if trunk.Enabled {
		m.log.Printf("Trunk %s: Re-registering after refresh", trunk.ID)
		return m.RegisterTrunk(trunk)
	}
	m.log.Printf("Trunk %s: Not re-registering (disabled)", trunk.ID)
	return nil
}

func (m *SIPTrunkManager) GetActiveTrunks() []*sip_trunk.SIPTrunk {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*sip_trunk.SIPTrunk, 0, len(m.trunks))
	for _, conn := range m.trunks {
		if conn.trunk.RegistrationStatus == sip_trunk.RegistrationStatusRegistered {
			result = append(result, conn.trunk)
		}
	}
	return result
}

func (m *SIPTrunkManager) GetTrunkStatus(trunkID string) (*sip_trunk.SIPTrunkStatusUpdate, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	conn, exists := m.trunks[trunkID]
	if !exists {
		trunk, err := m.repo.FindByID(trunkID)
		if err != nil {
			return nil, err
		}
		return &sip_trunk.SIPTrunkStatusUpdate{
			TrunkID:   trunkID,
			Status:    trunk.RegistrationStatus,
			Error:     trunk.LastError,
			Timestamp: trunk.UpdatedAt,
		}, nil
	}

	return &sip_trunk.SIPTrunkStatusUpdate{
		TrunkID:   trunkID,
		Status:    conn.trunk.RegistrationStatus,
		Error:     conn.trunk.LastError,
		Timestamp: time.Now(),
	}, nil
}

func (m *SIPTrunkManager) SelectTrunkForCall(trunkType sip_trunk.TrunkType) (*sip_trunk.SIPTrunk, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, conn := range m.trunks {
		if conn.trunk.TrunkType == trunkType &&
			conn.trunk.RegistrationStatus == sip_trunk.RegistrationStatusRegistered &&
			conn.trunk.Enabled {
			return conn.trunk, nil
		}
	}

	return nil, sip_trunk.ErrTrunkNotRegistered
}

func (m *SIPTrunkManager) SetStatusCallback(fn func(update sip_trunk.SIPTrunkStatusUpdate)) {
	m.statusCallback = fn
}

func (m *SIPTrunkManager) SetInboundInviteHandler(handler sip_trunk.InboundInviteHandler) {
	m.inboundMu.Lock()
	m.inboundHandler = handler
	m.inboundMu.Unlock()
}

func (m *SIPTrunkManager) inboundInviteHandler() sip_trunk.InboundInviteHandler {
	m.inboundMu.RLock()
	defer m.inboundMu.RUnlock()
	return m.inboundHandler
}

func (m *SIPTrunkManager) updateStatus(trunkID string, status sip_trunk.RegistrationStatus, errMsg string) {
	if err := m.repo.UpdateStatus(trunkID, status, errMsg); err != nil {
		m.log.Printf("Failed to update trunk %s status: %v", trunkID, err)
	}

	m.mu.RLock()
	if conn, exists := m.trunks[trunkID]; exists {
		conn.trunk.UpdateRegistrationStatus(status, errMsg)
	}
	m.mu.RUnlock()

	if m.statusCallback != nil {
		update := sip_trunk.SIPTrunkStatusUpdate{
			TrunkID:   trunkID,
			Status:    status,
			Error:     errMsg,
			Timestamp: time.Now(),
		}
		go m.statusCallback(update)
	}
}

func (m *SIPTrunkManager) createTrunkConnection(trunk *sip_trunk.SIPTrunk) (*trunkConnection, error) {
	m.log.Printf("Trunk %s: Starting connection setup (host=%s, port=%d, user=%s)", trunk.ID, trunk.Host, trunk.Port, trunk.Username)
	m.updateStatus(trunk.ID, sip_trunk.RegistrationStatusRegistering, "")

	listenPort, err := m.sipPortAllocator.allocate()
	if err != nil {
		return nil, fmt.Errorf("failed to allocate SIP port: %w", err)
	}
	m.log.Printf("Trunk %s: Allocated SIP port %d", trunk.ID, listenPort)

	runtime := newTrunkRuntimeConfig(trunk, m.defaults(), m.log)

	if runtime.BindPortOverride > 0 && runtime.BindPortOverride != listenPort {
		m.log.Printf("Trunk %s: overriding allocated port %d with operator-configured BindPort %d",
			trunk.ID, listenPort, runtime.BindPortOverride)
		m.sipPortAllocator.release(listenPort)
		listenPort = runtime.BindPortOverride
	}

	registrarAddr := formatRegistrarAddress(trunk.Host, trunk.Port)
	m.log.Printf("Trunk %s: Registrar address: %s", trunk.ID, registrarAddr)

	bindHost := "0.0.0.0"
	var outboundIP net.IP
	if runtime.BindHostOverride != "" {
		bindHost = runtime.BindHostOverride
		if ip := net.ParseIP(bindHost); ip != nil {
			outboundIP = ip
		}
		m.log.Printf("Trunk %s: using operator-configured BindHost %s", trunk.ID, bindHost)
	} else if outboundIPAddr, outboundErr := determineOutboundIP(registrarAddr); outboundErr == nil {
		bindHost = outboundIPAddr.String()
		outboundIP = outboundIPAddr
		m.log.Printf("Trunk %s: Detected outbound interface %s for registrar %s", trunk.ID, bindHost, registrarAddr)
	} else {
		m.log.Printf("Trunk %s: Failed to detect outbound interface: %v", trunk.ID, outboundErr)
	}

	var publicIP string
	if runtime.PublicAddress != "" {
		publicIP = stripHostPort(runtime.PublicAddress)
		m.log.Printf("Trunk %s: using operator-configured PublicAddress %s", trunk.ID, publicIP)
	} else {
		pubCtx, pubCancel := context.WithTimeout(context.Background(), m.defaults().PublicIPTimeout)
		ip, pubErr := detectPublicIP(pubCtx)
		pubCancel()
		if pubErr != nil {
			if outboundIP != nil {
				m.log.Printf("Trunk %s: Failed to detect public IP, using outbound interface: %v", trunk.ID, pubErr)
				ip = outboundIP.String()
			} else {
				m.sipPortAllocator.release(listenPort)
				return nil, fmt.Errorf("failed to detect public IP: %w", pubErr)
			}
		}
		publicIP = ip
	}

	externalIP := publicIP
	externalPort := listenPort
	if runtime.STUNEnabled && runtime.PublicAddress == "" {
		natCtx, natCancel := context.WithTimeout(context.Background(), m.defaults().STUNTimeout)
		mappedIP, mappedPort, natErr := discoverExternalEndpoint(natCtx, bindHost, listenPort, runtime.STUNServers...)
		natCancel()
		if natErr != nil {
			m.log.Printf("Trunk %s: STUN discovery skipped (non-fatal), using public IP %s:%d: %v", trunk.ID, publicIP, listenPort, natErr)
		} else {
			externalIP = mappedIP
			externalPort = mappedPort
			m.log.Printf("Trunk %s: Discovered external endpoint via STUN: %s:%d (listen %s:%d)",
				trunk.ID, externalIP, externalPort, bindHost, listenPort)
		}
	} else {
		m.log.Printf("Trunk %s: STUN discovery disabled (operator config); external %s:%d",
			trunk.ID, externalIP, externalPort)
	}

	ua, err := sipgo.NewUA(
		sipgo.WithUserAgentHostname(publicIP),
		sipgo.WithUserAgent(runtime.UserAgent),
	)
	if err != nil {
		m.sipPortAllocator.release(listenPort)
		return nil, fmt.Errorf("failed to create user agent: %w", err)
	}

	transportProtocol := "udp"
	if trunk.Transport != "" {
		transportProtocol = strings.ToLower(string(trunk.Transport))
	}
	if transportProtocol == "udp" {
		transportProtocol = "udp4"
	}

	transport := diago.Transport{
		Transport:    transportProtocol,
		BindHost:     bindHost,
		BindPort:     listenPort,
		ExternalHost: externalIP,
		ExternalPort: externalPort,
	}
	if ip := net.ParseIP(externalIP); ip != nil {
		transport.MediaExternalIP = ip
	}

	codecs := runtime.Codecs

	opts := []diago.DiagoOption{
		diago.WithTransport(transport),
	}
	if len(codecs) > 0 {
		mediaCfg := diago.MediaConfig{Codecs: codecs}

		switch runtime.SRTPMode {
		case sip_trunk.SRTPModeRequired, sip_trunk.SRTPModeOptional:
			mediaCfg.SecureRTPAlg = 1
		}
		opts = append(opts, diago.WithMediaConfig(mediaCfg))
	}

	client := diago.NewDiago(ua, opts...)
	m.log.Printf("Trunk %s: Starting diago background server", trunk.ID)

	ctx, cancel := context.WithCancel(m.ctx)
	conn := &trunkConnection{
		trunk:          trunk,
		ua:             ua,
		client:         client,
		ctx:            ctx,
		cancel:         cancel,
		listenPort:     listenPort,
		registered:     make(chan struct{}),
		runtime:        runtime,
		dialogs:        make(map[string]*diago.DialogClientSession),
		inboundDialogs: make(map[string]*diago.DialogServerSession),
	}

	if err := client.ServeBackground(ctx, func(inDialog *diago.DialogServerSession) {
		m.handleInboundDialog(conn, inDialog)
	}); err != nil {
		cancel()
		m.sipPortAllocator.release(listenPort)
		return nil, fmt.Errorf("failed to start diago: %w", err)
	}
	m.log.Printf("Trunk %s: Diago background server started", trunk.ID)

	m.log.Printf("Trunk %s: listening on %s:%d, external %s:%d", trunk.ID, bindHost, listenPort, externalIP, externalPort)

	conn.wg.Add(1)
	go func() {
		defer conn.wg.Done()
		m.runRegistration(conn, registrarAddr)
	}()

	return conn, nil
}

func (m *SIPTrunkManager) handleInboundDialog(conn *trunkConnection, dialog *diago.DialogServerSession) {
	if conn == nil || conn.trunk == nil || dialog == nil {
		return
	}
	m.log.Printf("Incoming dialog on trunk %s (port %d): %s", conn.trunk.ID, conn.listenPort, dialog.ID)

	sm := calls.NewCallStateMachine(m.callMetrics)
	sm.Dialing()

	adapter := &inboundDialogAdapter{manager: m, conn: conn, trunk: conn.trunk, dialog: dialog, callSM: sm}
	handler := m.inboundInviteHandler()
	if handler == nil {
		m.log.Printf("Trunk %s inbound dialog %s rejected: no inbound handler configured", conn.trunk.ID, dialog.ID)
		sm.LeaveDialing()
		sm.Finished()
		_ = adapter.Hangup(conn.ctx)
		return
	}

	workspaceID := strings.TrimSpace(conn.trunk.WorkspaceID)
	invite := sip_trunk.InboundInvite{
		ID:          dialog.ID,
		TrunkID:     conn.trunk.ID,
		WorkspaceID: workspaceID,
		FromNumber:  strings.TrimSpace(dialog.FromUser()),
		ToNumber:    strings.TrimSpace(dialog.ToUser()),
		ReceivedAt:  time.Now(),
		Trunk:       conn.trunk,
		Dialog:      adapter,
	}

	if err := handler.HandleInboundInvite(conn.ctx, invite); err != nil {
		m.log.Printf("Trunk %s inbound dialog %s ended with error: %v", conn.trunk.ID, dialog.ID, err)
		_ = adapter.Hangup(context.Background())
	}
	// CRITICAL lifecycle marker: returning from this serve handler makes diago
	// send BYE to the caller and close the dialog/media (diago.go serve loop).
	// A surrendered (human-attended) call must therefore NOT let this return
	// until the human leg ends — see InboundReceptiveBridgeUseCase.holdUntilHumanHangup.
	m.log.Printf("[media-handoff] Trunk %s inbound dialog %s: serve handler returning — diago will now hang up the caller + close the dialog/media", conn.trunk.ID, dialog.ID)
	adapter.untrack()
}

type inboundDialogAdapter struct {
	manager *SIPTrunkManager
	conn    *trunkConnection
	trunk   *sip_trunk.SIPTrunk
	dialog  *diago.DialogServerSession
	callSM  *calls.CallStateMachine
}

func (a *inboundDialogAdapter) ID() string {
	if a == nil || a.dialog == nil {
		return ""
	}
	return a.dialog.ID
}

func (a *inboundDialogAdapter) FromUser() string {
	if a == nil || a.dialog == nil {
		return ""
	}
	return a.dialog.FromUser()
}

func (a *inboundDialogAdapter) ToUser() string {
	if a == nil || a.dialog == nil {
		return ""
	}
	return a.dialog.ToUser()
}

func (a *inboundDialogAdapter) Trying() error {
	if a == nil || a.dialog == nil {
		return nil
	}
	return a.dialog.Trying()
}

func (a *inboundDialogAdapter) Ringing() error {
	if a == nil || a.dialog == nil {
		return nil
	}
	return a.dialog.Ringing()
}

func (a *inboundDialogAdapter) Answer(ctx context.Context, recording *voip.RecordingMeta) (sip_trunk.TrunkCallSession, error) {
	if a == nil || a.dialog == nil || a.conn == nil || a.trunk == nil {
		return sip_trunk.TrunkCallSession{}, errors.New("inbound dialog adapter is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := a.dialog.Answer(); err != nil {
		a.callSM.LeaveDialing()
		a.callSM.Finished()
		return sip_trunk.TrunkCallSession{}, err
	}

	mediaSession := a.dialog.MediaSession()
	mediaWaitDeadline := time.Now().Add(3 * time.Second)
	for mediaSession == nil && time.Now().Before(mediaWaitDeadline) {
		select {
		case <-ctx.Done():
			a.callSM.LeaveDialing()
			a.callSM.Finished()
			_ = a.dialog.Close()
			return sip_trunk.TrunkCallSession{}, fmt.Errorf("answer inbound call: context done while waiting for media session: %w", ctx.Err())
		case <-time.After(50 * time.Millisecond):
			mediaSession = a.dialog.MediaSession()
		}
	}
	if mediaSession == nil {
		a.callSM.LeaveDialing()
		a.callSM.Finished()
		_ = a.dialog.Close()
		return sip_trunk.TrunkCallSession{}, errors.New("answer inbound call: no media session available")
	}

	a.track()

	if err := WrapMediaSessionForLatching(mediaSession, LatchOptions{
		ExpectedPTs: PayloadTypesFromCodecs(mediaSession.Codecs),
		TrunkID:     a.trunk.ID,
		CallID:      a.dialog.ID,
		Logger:      a.manager.log,
	}); err != nil && a.manager != nil {
		a.manager.log.Printf("Trunk %s inbound call %s: RTP latching unavailable (%v); proceeding without it",
			a.trunk.ID, a.dialog.ID, err)
	}

	localAddr := mediaSession.Laddr.String()
	remoteAddr := mediaSession.Raddr.String()

	adapter := &mediaSessionAdapter{session: mediaSession, diagLog: a.manager.log, diagID: a.dialog.ID}

	reorderBuf := audio.NewRTPReorderBuffer(adapter, audio.RTPReorderBufferOptions{
		Depth:   3,
		MaxWait: 60 * time.Millisecond,
		Logger:  a.manager.log.Printf,
		CallID:  string(a.dialog.ID),
	})
	reorderBuf.Run(ctx)
	var wrappedMedia voip.MediaSession = reorderBuf
	if a.manager.mediaSessionRecorder != nil {
		meta := voip.RecordingMeta{}
		if recording != nil {
			meta = *recording
		}
		wrappedMedia = a.manager.mediaSessionRecorder(reorderBuf, string(a.dialog.ID), meta)
	}

	a.callSM.Answered()

	a.dialog.OnClose(func() error {
		if a.manager != nil && a.manager.log != nil {
			a.manager.log.Printf("[media-handoff] inbound dialog %s OnClose fired — closing reorder buffer + media session (call is ending)", a.dialog.ID)
		}
		a.callSM.LeaveOngoing()
		a.callSM.Finished()
		reorderBuf.Close()
		_ = wrappedMedia.Close()
		return nil
	})

	session := sip_trunk.TrunkCallSession{
		ID:           a.dialog.ID,
		TrunkID:      a.trunk.ID,
		PhoneNumber:  strings.TrimSpace(a.dialog.FromUser()),
		Direction:    "inbound",
		StartedAt:    time.Now(),
		LocalAddr:    localAddr,
		RemoteAddr:   remoteAddr,
		MediaSession: wrappedMedia,
		Media:        buildMediaInfo(mediaSession),
	}

	return session, nil
}

func (a *inboundDialogAdapter) Hangup(ctx context.Context) error {
	if a == nil || a.dialog == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return a.dialog.Hangup(ctx)
}

func (a *inboundDialogAdapter) track() {
	if a == nil || a.conn == nil || a.dialog == nil {
		return
	}
	a.conn.dialogsMu.Lock()
	a.conn.inboundDialogs[a.dialog.ID] = a.dialog
	a.conn.dialogsMu.Unlock()
}

func (a *inboundDialogAdapter) untrack() {
	if a == nil || a.conn == nil || a.dialog == nil {
		return
	}
	a.conn.dialogsMu.Lock()
	delete(a.conn.inboundDialogs, a.dialog.ID)
	a.conn.dialogsMu.Unlock()
}

func (m *SIPTrunkManager) runRegistration(conn *trunkConnection, registrarAddr string) {

	if !conn.runtime.RegisterEnabled {
		m.log.Printf("Trunk %s: registration disabled by configuration; running inbound-only", conn.trunk.ID)
		m.updateStatus(conn.trunk.ID, sip_trunk.RegistrationStatusRegistered, "")
		conn.regOnce.Do(func() { close(conn.registered) })
		<-conn.ctx.Done()
		return
	}

	domain := conn.trunk.Domain
	if domain == "" {
		domain = strings.Split(conn.trunk.Host, ":")[0]
	}

	identity := sip.Uri{User: conn.trunk.Username, Host: domain}
	proxyHost := registrarAddr
	if conn.runtime.OutboundProxy != "" {
		proxyHost = conn.runtime.OutboundProxy
	}
	regOpts := diago.RegisterOptions{
		Username:      conn.runtime.AuthUsername,
		Password:      conn.trunk.Password,
		ProxyHost:     proxyHost,
		Expiry:        conn.runtime.RegisterExpiry,
		RetryInterval: conn.runtime.RegisterRetryDelay,
		AllowHeaders:  conn.runtime.AllowRegHeaders,
	}

	m.log.Printf("Trunk %s: Starting SIP registration with %s (user=%s, domain=%s)",
		conn.trunk.ID, registrarAddr, conn.trunk.Username, domain)

	t, err := conn.client.RegisterTransaction(conn.ctx, identity, regOpts)
	if err != nil {
		m.log.Printf("Trunk %s: Failed to create register transaction: %v", conn.trunk.ID, err)
		m.updateStatus(conn.trunk.ID, sip_trunk.RegistrationStatusFailed, err.Error())
		conn.regOnce.Do(func() { close(conn.registered) })
		return
	}

	if err := t.Register(conn.ctx); err != nil {
		m.log.Printf("Trunk %s: Initial registration failed: %v", conn.trunk.ID, err)
		m.updateStatus(conn.trunk.ID, sip_trunk.RegistrationStatusFailed, err.Error())
		conn.regOnce.Do(func() { close(conn.registered) })

		for {
			select {
			case <-conn.ctx.Done():
				m.log.Printf("Trunk %s: Registration stopped (context canceled)", conn.trunk.ID)
				return
			case <-time.After(conn.runtime.RegisterRetryDelay):
				m.log.Printf("Trunk %s: Retrying registration...", conn.trunk.ID)
			}

			if err := t.Register(conn.ctx); err != nil {
				if errors.Is(err, context.Canceled) {
					m.log.Printf("Trunk %s: Registration stopped (context canceled)", conn.trunk.ID)
					return
				}
				m.log.Printf("Trunk %s: Registration retry failed: %v", conn.trunk.ID, err)
				m.updateStatus(conn.trunk.ID, sip_trunk.RegistrationStatusFailed, err.Error())
				continue
			}
			break
		}
	}

	m.log.Printf("Trunk %s: Registration successful", conn.trunk.ID)
	m.updateStatus(conn.trunk.ID, sip_trunk.RegistrationStatusRegistered, "")
	conn.regOnce.Do(func() { close(conn.registered) })

	defer func() {
		unregCtx, unregCancel := context.WithTimeout(context.Background(), conn.runtime.UnregisterTimeout)
		defer unregCancel()
		if err := t.Unregister(unregCtx); err != nil {
			m.log.Printf("Trunk %s: Failed to unregister: %v", conn.trunk.ID, err)
		} else {
			m.log.Printf("Trunk %s: Unregistered successfully", conn.trunk.ID)
		}
	}()

	for i := 0; ; i++ {
		err := t.QualifyLoop(conn.ctx)
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			m.log.Printf("Trunk %s: Registration stopped (context canceled)", conn.trunk.ID)
			return
		}

		if err != nil {
			m.log.Printf("Trunk %s: Registration qualify failed (attempt %d): %v", conn.trunk.ID, i, err)
			m.updateStatus(conn.trunk.ID, sip_trunk.RegistrationStatusFailed, err.Error())

			select {
			case <-time.After(conn.runtime.RegisterRetryDelay):
				m.log.Printf("Trunk %s: Re-registering...", conn.trunk.ID)
			case <-conn.ctx.Done():
				return
			}

			if err := t.Register(conn.ctx); err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}
				m.log.Printf("Trunk %s: Re-registration failed: %v", conn.trunk.ID, err)
				continue
			}
			m.updateStatus(conn.trunk.ID, sip_trunk.RegistrationStatusRegistered, "")
		}
	}
}

func formatRegistrarAddress(host string, port int) string {
	if port == 0 {
		port = 5060
	}
	return fmt.Sprintf("%s:%d", host, port)
}
