package voipinfra

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/emiago/diago"
	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"

	"vozko/domain/sip_trunk"
	"vozko/infra/natdiscovery"
)

type trunkConnection struct {
	manager        *SIPTrunkManager
	trunk          *sip_trunk.SIPTrunk
	runtime        trunkRuntimeConfig
	client         *diago.Diago
	registrar      string
	destination    string
	inboundSources sip_trunk.InboundSources
	listenPort     int
	allocatedPort  bool

	ctx       context.Context
	cancel    context.CancelFunc
	workersMu sync.Mutex
	closing   bool
	workers   sync.WaitGroup

	statusMu  sync.RWMutex
	status    sip_trunk.RegistrationStatus
	lastError string
	statusAt  time.Time

	callsMu sync.Mutex
	calls   map[string]*trackedCall
}

type endpoints struct {
	bindHost     string
	listenPort   int
	allocated    bool
	externalHost string
	externalPort int
}

func (m *SIPTrunkManager) connect(trunk *sip_trunk.SIPTrunk) (*trunkConnection, error) {
	runtime, err := newTrunkRuntimeConfig(trunk, m.defaults())
	if err != nil {
		return nil, err
	}
	registrar := registrarAddress(trunk)
	ep, err := m.resolveEndpoints(trunk, runtime, registrar)
	if err != nil {
		return nil, err
	}
	releasePort := func() {
		if ep.allocated {
			m.ports.release(ep.listenPort)
		}
	}

	ua, err := sipgo.NewUA(sipgo.WithUserAgentHostname(ep.externalHost), sipgo.WithUserAgent(runtime.UserAgent))
	if err != nil {
		releasePort()
		return nil, fmt.Errorf("create SIP user agent: %w", err)
	}
	transport := diago.Transport{
		Transport:    runtime.Transport,
		BindHost:     ep.bindHost,
		BindPort:     ep.listenPort,
		ExternalHost: ep.externalHost,
		ExternalPort: ep.externalPort,
	}
	if ip := net.ParseIP(ep.externalHost); ip != nil {
		transport.MediaExternalIP = ip
	}
	if runtime.SRTPMode.Offers() {
		transport.MediaSRTP = 1
	}
	options := []diago.DiagoOption{diago.WithTransport(transport)}
	if len(runtime.Codecs) > 0 {
		options = append(options, diago.WithMediaConfig(diago.MediaConfig{Codecs: runtime.Codecs}))
	}

	destination := registrar
	if runtime.OutboundProxy != "" {
		destination = runtime.OutboundProxy
	}
	ctx, cancel := context.WithCancel(m.ctx)
	conn := &trunkConnection{
		manager:        m,
		trunk:          trunk,
		runtime:        runtime,
		client:         diago.NewDiago(ua, options...),
		registrar:      registrar,
		destination:    destination,
		inboundSources: runtime.InboundSources.With(resolveHosts(ctx, registrar, destination)...),
		listenPort:     ep.listenPort,
		allocatedPort:  ep.allocated,
		ctx:            ctx,
		cancel:         cancel,
		status:         sip_trunk.RegistrationStatusRegistering,
		statusAt:       time.Now(),
		calls:          make(map[string]*trackedCall),
	}
	if err := conn.client.ServeBackground(ctx, func(dialog *diago.DialogServerSession) {
		m.handleInboundDialog(conn, dialog)
	}); err != nil {
		cancel()
		_ = ua.Close()
		releasePort()
		return nil, fmt.Errorf("listen on %s:%d: %w", ep.bindHost, ep.listenPort, err)
	}
	m.log.Printf("trunk %s: listening on %s/%s:%d, advertised %s:%d, registrar %s", trunk.ID, runtime.Transport, ep.bindHost, ep.listenPort, ep.externalHost, ep.externalPort, registrar)
	return conn, nil
}

func (m *SIPTrunkManager) resolveEndpoints(trunk *sip_trunk.SIPTrunk, runtime trunkRuntimeConfig, registrar string) (endpoints, error) {
	ep := endpoints{bindHost: m.cfg.SIPBindHost}
	var interfaceIP net.IP
	if runtime.BindHost != "" {
		ep.bindHost = runtime.BindHost
		interfaceIP = net.ParseIP(runtime.BindHost)
	} else if ip, err := natdiscovery.OutboundIP(registrar); err == nil {
		ep.bindHost = ip.String()
		interfaceIP = ip
	} else {
		m.log.Printf("trunk %s: outbound interface lookup for %s failed, binding %s: %v", trunk.ID, registrar, ep.bindHost, err)
	}

	if runtime.BindPort > 0 {
		ep.listenPort = runtime.BindPort
	} else {
		port, err := m.ports.allocate()
		if err != nil {
			return endpoints{}, err
		}
		ep.listenPort, ep.allocated = port, true
	}
	ep.externalPort = ep.listenPort

	switch {
	case runtime.PublicAddress != "":
		ep.externalHost = stripHostPort(runtime.PublicAddress)
	case runtime.STUNEnabled:
		ctx, cancel := context.WithTimeout(m.ctx, stunTimeout)
		mapped, err := natdiscovery.PublicEndpoint(ctx, ep.bindHost, ep.listenPort, runtime.STUNServers)
		cancel()
		if err == nil {
			ep.externalHost, ep.externalPort = mapped.IP.String(), mapped.Port
			break
		}
		m.log.Printf("trunk %s: STUN mapping failed, using the host public address: %v", trunk.ID, err)
		fallthrough
	default:
		ip, err := m.publicIP.resolve(m.ctx)
		switch {
		case err == nil:
			ep.externalHost = ip
		case interfaceIP != nil && !interfaceIP.IsUnspecified():
			m.log.Printf("trunk %s: public address discovery failed, advertising interface %s: %v", trunk.ID, interfaceIP, err)
			ep.externalHost = interfaceIP.String()
		default:
			if ep.allocated {
				m.ports.release(ep.listenPort)
			}
			return endpoints{}, fmt.Errorf("resolve public address: %w", err)
		}
	}
	return ep, nil
}

func resolveHosts(ctx context.Context, addresses ...string) []net.IP {
	lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var ips []net.IP
	for _, address := range addresses {
		host := stripHostPort(address)
		if ip := net.ParseIP(host); ip != nil {
			ips = append(ips, ip)
			continue
		}
		resolved, err := net.DefaultResolver.LookupIP(lookupCtx, "ip", host)
		if err == nil {
			ips = append(ips, resolved...)
		}
	}
	return ips
}

func (c *trunkConnection) startRegistration() {
	c.spawn(c.runRegistration)
}

func (c *trunkConnection) spawn(work func()) bool {
	c.workersMu.Lock()
	defer c.workersMu.Unlock()
	if c.closing {
		return false
	}
	c.workers.Add(1)
	go func() {
		defer c.workers.Done()
		work()
	}()
	return true
}

func (c *trunkConnection) markClosing() {
	c.workersMu.Lock()
	c.closing = true
	c.workersMu.Unlock()
}

func (c *trunkConnection) runRegistration() {
	log := c.manager.log
	if !c.runtime.RegisterEnabled {
		log.Printf("trunk %s: registration disabled, accepting calls without REGISTER", c.trunk.ID)
		c.setStatus(sip_trunk.RegistrationStatusRegistered, "")
		return
	}
	identity := sip.Uri{User: c.trunk.Username, Host: c.trunk.SignalingDomain()}
	tx, err := c.client.RegisterTransaction(c.ctx, identity, diago.RegisterOptions{
		Username:     c.runtime.AuthUsername,
		Password:     c.trunk.Password,
		ProxyHost:    c.destination,
		Expiry:       c.runtime.RegisterExpiry,
		AllowHeaders: c.runtime.AllowRegHeaders,
	})
	if err != nil {
		c.setStatus(sip_trunk.RegistrationStatusFailed, err.Error())
		return
	}
	registered := false
	defer func() {
		if !registered {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), c.runtime.UnregisterTimeout)
		defer cancel()
		if err := tx.Unregister(ctx); err != nil {
			log.Printf("trunk %s: unregister failed: %v", c.trunk.ID, err)
		}
	}()

	for {
		if err := tx.Register(c.ctx); err != nil {
			if c.ctx.Err() != nil {
				return
			}
			log.Printf("trunk %s: registration failed: %v", c.trunk.ID, err)
			c.setStatus(sip_trunk.RegistrationStatusFailed, err.Error())
			if !c.sleep(c.runtime.RegisterRetryDelay) {
				return
			}
			continue
		}
		registered = true
		c.setStatus(sip_trunk.RegistrationStatusRegistered, "")
		err := tx.QualifyLoop(c.ctx)
		if c.ctx.Err() != nil || errors.Is(err, context.Canceled) {
			return
		}
		log.Printf("trunk %s: registration refresh failed: %v", c.trunk.ID, err)
		c.setStatus(sip_trunk.RegistrationStatusFailed, errorText(err))
		if !c.sleep(c.runtime.RegisterRetryDelay) {
			return
		}
	}
}

func errorText(err error) string {
	if err == nil {
		return "registration refresh stopped"
	}
	return err.Error()
}

func (c *trunkConnection) sleep(d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-c.ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (c *trunkConnection) setStatus(status sip_trunk.RegistrationStatus, lastError string) {
	c.statusMu.Lock()
	changed := c.status != status || c.lastError != lastError
	c.status, c.lastError, c.statusAt = status, lastError, time.Now()
	c.statusMu.Unlock()
	if changed {
		c.manager.persistStatus(c.trunk.ID, status, lastError)
	}
}

func (c *trunkConnection) statusUpdate() sip_trunk.SIPTrunkStatusUpdate {
	c.statusMu.RLock()
	defer c.statusMu.RUnlock()
	return sip_trunk.SIPTrunkStatusUpdate{TrunkID: c.trunk.ID, Status: c.status, Error: c.lastError, Timestamp: c.statusAt}
}

func (c *trunkConnection) registered() bool {
	c.statusMu.RLock()
	defer c.statusMu.RUnlock()
	return c.status == sip_trunk.RegistrationStatusRegistered
}

type publicIPResolver struct {
	override string
	servers  []string

	mu     sync.Mutex
	cached string
}

func newPublicIPResolver(override string, servers []string) *publicIPResolver {
	return &publicIPResolver{override: stripHostPort(override), servers: servers}
}

func (r *publicIPResolver) resolve(ctx context.Context) (string, error) {
	if r.override != "" {
		return r.override, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cached != "" {
		return r.cached, nil
	}
	lookupCtx, cancel := context.WithTimeout(ctx, publicIPTimeout)
	defer cancel()
	ip, err := natdiscovery.PublicIP(lookupCtx, r.servers)
	if err != nil {
		return "", err
	}
	r.cached = ip
	return ip, nil
}
