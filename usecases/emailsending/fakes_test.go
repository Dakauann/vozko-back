package emailsending

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"vozko/domain/emailaddress"
	es "vozko/domain/emailsending"
	"vozko/domain/emailtoken"
	"vozko/domain/notification"
)

var testNow = time.Date(2026, 10, 21, 15, 0, 0, 0, time.UTC)

type memAccounts struct {
	mu   sync.Mutex
	rows map[string]es.SenderAccount
	fail error
}

func (m *memAccounts) GetByWorkspace(_ context.Context, ws string) (*es.SenderAccount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, a := range m.rows {
		if a.WorkspaceID == ws {
			v := a
			return &v, nil
		}
	}
	return nil, es.ErrAccountNotFound
}

func (m *memAccounts) GetByTenantName(_ context.Context, name string) (*es.SenderAccount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, a := range m.rows {
		if a.TenantName == name {
			v := a
			return &v, nil
		}
	}
	return nil, es.ErrAccountNotFound
}

func (m *memAccounts) Create(_ context.Context, a *es.SenderAccount) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	for _, existing := range m.rows {
		if existing.WorkspaceID == a.WorkspaceID {
			return errors.New("duplicate key")
		}
		if existing.Slug == a.Slug {
			return es.ErrSlugTaken
		}
	}
	if a.ID == "" {
		a.ID = uuid.NewString()
	}
	m.rows[a.ID] = *a
	return nil
}

func (m *memAccounts) Update(_ context.Context, a *es.SenderAccount) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rows[a.ID]; !ok {
		return es.ErrAccountNotFound
	}
	m.rows[a.ID] = *a
	return nil
}

func (m *memAccounts) ChangeSlug(_ context.Context, id, slug string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.rows[id]
	if !ok {
		return es.ErrAccountNotFound
	}
	for otherID, other := range m.rows {
		if otherID != id && other.Slug == slug {
			return es.ErrSlugTaken
		}
	}
	a.Slug = slug
	m.rows[id] = a
	return nil
}

func (m *memAccounts) SlugTaken(_ context.Context, slug string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, a := range m.rows {
		if a.Slug == slug {
			return true, nil
		}
	}
	return false, nil
}

func (m *memAccounts) ListByMode(_ context.Context, mode es.Mode) ([]es.SenderAccount, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []es.SenderAccount
	for _, a := range m.rows {
		if a.Mode == mode {
			out = append(out, a)
		}
	}
	return out, nil
}

func (m *memAccounts) List(_ context.Context, offset, limit int) ([]es.SenderAccount, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []es.SenderAccount
	for _, a := range m.rows {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].WorkspaceID < out[j].WorkspaceID })
	return out, int64(len(out)), nil
}

type memDomains struct {
	mu         sync.Mutex
	rows       map[string]es.SendingDomain
	seq        int
	staleReads int
}

func (m *memDomains) find(pred func(es.SendingDomain) bool) (*es.SendingDomain, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, d := range m.rows {
		if pred(d) {
			v := d
			v.Records = append([]es.DNSRecord(nil), d.Records...)
			return &v, nil
		}
	}
	return nil, es.ErrDomainNotFound
}

func (m *memDomains) Get(_ context.Context, ws, id string) (*es.SendingDomain, error) {
	return m.find(func(d es.SendingDomain) bool { return d.WorkspaceID == ws && d.ID == id })
}

func (m *memDomains) GetByDomain(_ context.Context, domain string) (*es.SendingDomain, error) {
	m.mu.Lock()
	stale := m.staleReads > 0
	if stale {
		m.staleReads--
	}
	m.mu.Unlock()
	if stale {
		return nil, es.ErrDomainNotFound
	}
	return m.find(func(d es.SendingDomain) bool { return d.Domain == domain })
}

func (m *memDomains) GetByReplyDomain(_ context.Context, reply string) (*es.SendingDomain, error) {
	return m.find(func(d es.SendingDomain) bool { return d.ReplyDomain == reply })
}

func (m *memDomains) list(pred func(es.SendingDomain) bool) []es.SendingDomain {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []es.SendingDomain
	for _, d := range m.rows {
		if pred(d) {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (m *memDomains) ListByWorkspace(_ context.Context, ws string) ([]es.SendingDomain, error) {
	return m.list(func(d es.SendingDomain) bool { return d.WorkspaceID == ws }), nil
}

func (m *memDomains) ListPending(_ context.Context, _ int) ([]es.SendingDomain, error) {
	return m.list(func(d es.SendingDomain) bool {
		return (!d.Retired && (d.Status == es.DomainPending || d.Status == es.DomainVerifying)) || d.AwaitsRelease()
	}), nil
}

func (m *memDomains) ListVerified(_ context.Context, kind es.DomainKind, offset, limit int) ([]es.SendingDomain, error) {
	all := m.list(func(d es.SendingDomain) bool { return d.Kind == kind && d.Status == es.DomainVerified && !d.Retired })
	if offset >= len(all) {
		return nil, nil
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	return all[offset:end], nil
}

func (m *memDomains) Create(_ context.Context, d *es.SendingDomain) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, existing := range m.rows {
		if existing.Domain == d.Domain || existing.ReplyDomain == d.ReplyDomain {
			return es.ErrDomainTaken
		}
	}
	m.seq++
	if d.ID == "" {
		d.ID = fmt.Sprintf("dom-%02d", m.seq)
	}
	d.CreatedAt = testNow
	m.rows[d.ID] = *d
	return nil
}

func (m *memDomains) Update(ctx context.Context, d *es.SendingDomain) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	stored, ok := m.rows[d.ID]
	if !ok {
		return es.ErrDomainNotFound
	}
	next := *d
	next.ReleaseState = stored.ReleaseState
	if stored.ReleaseState != es.ReleaseNone {
		next.Retired = stored.Retired
	}
	m.rows[d.ID] = next
	return nil
}

func (m *memDomains) SetRelease(ctx context.Context, d *es.SendingDomain) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	stored, ok := m.rows[d.ID]
	if !ok {
		return es.ErrDomainNotFound
	}
	stored.ReleaseState = d.ReleaseState
	stored.Retired = d.ReleaseState != es.ReleaseNone
	m.rows[d.ID] = stored
	return nil
}

type memSenders struct {
	mu   sync.Mutex
	rows map[string]es.Sender
}

func (m *memSenders) Get(_ context.Context, ws, id string) (*es.Sender, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.rows[id]
	if !ok || s.WorkspaceID != ws {
		return nil, es.ErrSenderNotFound
	}
	return &s, nil
}

func (m *memSenders) GetDefault(_ context.Context, ws string) (*es.Sender, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.rows {
		if s.WorkspaceID == ws && s.IsDefault {
			v := s
			return &v, nil
		}
	}
	return nil, es.ErrSenderNotFound
}

func (m *memSenders) ListByWorkspace(_ context.Context, ws string) ([]es.Sender, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []es.Sender
	for _, s := range m.rows {
		if s.WorkspaceID == ws {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LocalPart < out[j].LocalPart })
	return out, nil
}

func (m *memSenders) save(s *es.Sender) error {
	for _, existing := range m.rows {
		if existing.ID != s.ID && existing.DomainID == s.DomainID && existing.LocalPart == s.LocalPart {
			return es.ErrSenderExists
		}
	}
	if s.IsDefault {
		for id, existing := range m.rows {
			if existing.WorkspaceID == s.WorkspaceID && id != s.ID {
				existing.IsDefault = false
				m.rows[id] = existing
			}
		}
	}
	m.rows[s.ID] = *s
	return nil
}

func (m *memSenders) Create(_ context.Context, s *es.Sender) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s.ID == "" {
		s.ID = uuid.NewString()
	}
	return m.save(s)
}

func (m *memSenders) Update(_ context.Context, s *es.Sender) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rows[s.ID]; !ok {
		return es.ErrSenderNotFound
	}
	return m.save(s)
}

func (m *memSenders) Delete(_ context.Context, ws, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.rows[id]
	if !ok || s.WorkspaceID != ws {
		return es.ErrSenderNotFound
	}
	delete(m.rows, id)
	return nil
}

func (m *memSenders) MoveDomain(_ context.Context, ws, from, to string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, s := range m.rows {
		if s.WorkspaceID == ws && s.DomainID == from {
			s.DomainID = to
			m.rows[id] = s
		}
	}
	return nil
}

type memUsage struct {
	mu        sync.Mutex
	used      map[string]int64
	takeErr   error
	givebacks int
}

func (m *memUsage) key(domainID, day string) string { return domainID + "|" + day }

func (m *memUsage) TakeDailySlot(_ context.Context, domainID, day string, capValue int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.takeErr != nil {
		return false, m.takeErr
	}
	k := m.key(domainID, day)
	if capValue == 0 || (capValue != es.Uncapped && m.used[k] >= capValue) {
		return false, nil
	}
	m.used[k]++
	return true, nil
}

func (m *memUsage) GiveBackDailySlot(_ context.Context, domainID, day string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.givebacks++
	if m.used[m.key(domainID, day)] > 0 {
		m.used[m.key(domainID, day)]--
	}
	return nil
}

func (m *memUsage) Used(_ context.Context, domainID, day string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.used[m.key(domainID, day)], nil
}

type memSuppressions struct {
	mu        sync.Mutex
	rows      map[string]es.Suppression
	adds      int
	lastQuery es.SuppressionQuery
}

func (m *memSuppressions) Suppress(_ context.Context, s es.Suppression) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.adds++
	k := s.WorkspaceID + "|" + string(s.Address)
	if _, ok := m.rows[k]; !ok {
		m.rows[k] = s
	}
	return nil
}

func (m *memSuppressions) Remove(_ context.Context, ws string, a emailaddress.Address) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rows, ws+"|"+string(a))
	return nil
}

func (m *memSuppressions) Suppressed(_ context.Context, ws string, addrs []emailaddress.Address) (map[emailaddress.Address]es.SuppressReason, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[emailaddress.Address]es.SuppressReason{}
	for _, a := range addrs {
		if s, ok := m.rows[ws+"|"+string(a)]; ok {
			out[a] = s.Reason
		}
	}
	return out, nil
}

func (m *memSuppressions) List(_ context.Context, ws string, q es.SuppressionQuery) ([]es.Suppression, int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastQuery = q
	var out []es.Suppression
	for _, s := range m.rows {
		if s.WorkspaceID == ws {
			out = append(out, s)
		}
	}
	return out, int64(len(out)), nil
}

type memMessages struct {
	mu        sync.Mutex
	rows      map[string]es.Message
	events    map[string]bool
	updateErr error
}

func (m *memMessages) Record(_ context.Context, msg *es.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if msg.ID == "" {
		msg.ID = uuid.NewString()
	}
	m.rows[msg.ID] = *msg
	return nil
}

func (m *memMessages) Get(_ context.Context, id string) (*es.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	msg, ok := m.rows[id]
	if !ok {
		return nil, es.ErrMessageNotFound
	}
	return &msg, nil
}

func (m *memMessages) GetByProviderID(_ context.Context, pid string) (*es.Message, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, msg := range m.rows {
		if pid != "" && msg.ProviderMessageID == pid {
			v := msg
			return &v, nil
		}
	}
	return nil, es.ErrMessageNotFound
}

func (m *memMessages) Update(_ context.Context, msg *es.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.updateErr != nil {
		return m.updateErr
	}
	if _, ok := m.rows[msg.ID]; !ok {
		return es.ErrMessageNotFound
	}
	m.rows[msg.ID] = *msg
	return nil
}

func (m *memMessages) AppendEvent(_ context.Context, id string, ev es.DeliveryEvent) (es.AppendedEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	msg, ok := m.rows[id]
	if !ok {
		return es.AppendedEvent{}, es.ErrMessageNotFound
	}
	key := id + "|" + string(ev.Kind) + "|" + ev.OccurredAt.String() + "|" + ev.Link
	if m.events[key] {
		return es.AppendedEvent{Message: msg}, nil
	}
	m.events[key] = true
	effect := es.ApplyEvent(&msg, ev)
	m.rows[id] = msg
	return es.AppendedEvent{New: true, Message: msg, Effect: effect}, nil
}

func (m *memMessages) HealthWindow(_ context.Context, _ string, _ time.Time) (es.HealthWindow, error) {
	return es.HealthWindow{Delivered: 400, HardBounces: 2}, nil
}

type fakeProvider struct {
	mu            sync.Mutex
	tenantErr     error
	identityErr   error
	sendErr       error
	states        map[string]es.IdentityState
	health        es.TenantHealth
	healthErr     error
	sent          []es.OutboundMessage
	identities    []string
	removedIdents []string
	sendingCalls  []bool
	suppressed    []string
	suppressErr   error
	unsuppressed  []string
	statusCalls   int
	healthCalls   int
	tenantCalls   int
	onIdentity    func()
	lanes         map[string][]es.ProviderLane
}

func (p *fakeProvider) lane(ctx context.Context, call string) {
	if p.lanes == nil {
		p.lanes = map[string][]es.ProviderLane{}
	}
	p.lanes[call] = append(p.lanes[call], es.ProviderLaneOf(ctx))
}

func (p *fakeProvider) lanesOf(call string) []es.ProviderLane {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]es.ProviderLane(nil), p.lanes[call]...)
}

func (p *fakeProvider) Name() string { return "fake" }

func (p *fakeProvider) EnsureTenant(ctx context.Context, spec es.TenantSpec) (es.Tenant, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tenantCalls++
	p.lane(ctx, "ensure tenant")
	if p.tenantErr != nil {
		return es.Tenant{}, p.tenantErr
	}
	return es.Tenant{Name: spec.Name, Ref: "arn:tenant/" + spec.Name}, nil
}

func (p *fakeProvider) EnsureIdentity(ctx context.Context, spec es.IdentitySpec) (es.IdentitySetup, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lane(ctx, "ensure identity")
	if p.onIdentity != nil {
		p.onIdentity()
	}
	if p.identityErr != nil {
		return es.IdentitySetup{}, p.identityErr
	}
	p.identities = append(p.identities, spec.Domain)
	return es.IdentitySetup{
		Records: []es.DNSRecord{
			{Purpose: es.RecordDKIM, Type: "CNAME", Name: "k1._domainkey." + spec.Domain, Value: "k1.dkim.test"},
			{Purpose: es.RecordMailFromMX, Type: "MX", Name: spec.MailFromDomain, Value: "feedback.test", Priority: 10},
			{Purpose: es.RecordMailFromSPF, Type: "TXT", Name: spec.MailFromDomain, Value: "v=spf1 include:test ~all"},
		},
		InboundHost: "inbound.test",
	}, nil
}

func (p *fakeProvider) IdentityStatus(ctx context.Context, domain string) (es.IdentityState, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.statusCalls++
	p.lane(ctx, "identity status")
	return p.states[domain], nil
}

func (p *fakeProvider) RemoveIdentity(ctx context.Context, _, domain string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lane(ctx, "remove identity")
	p.removedIdents = append(p.removedIdents, domain)
	return nil
}

func (p *fakeProvider) Send(_ context.Context, msg es.OutboundMessage) (es.SendReceipt, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sendErr != nil {
		return es.SendReceipt{}, p.sendErr
	}
	p.sent = append(p.sent, msg)
	return es.SendReceipt{ProviderMessageID: fmt.Sprintf("ses-%d", len(p.sent))}, nil
}

func (p *fakeProvider) SetTenantSending(_ context.Context, _ string, enabled bool) error {
	p.sendingCalls = append(p.sendingCalls, enabled)
	return nil
}

func (p *fakeProvider) TenantHealth(ctx context.Context, _ string) (es.TenantHealth, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.healthCalls++
	p.lane(ctx, "tenant health")
	return p.health, p.healthErr
}

func (p *fakeProvider) SuppressAddress(_ context.Context, _ string, a emailaddress.Address, _ es.SuppressReason) error {
	if p.suppressErr != nil {
		return p.suppressErr
	}
	p.suppressed = append(p.suppressed, string(a))
	return nil
}

func (p *fakeProvider) UnsuppressAddress(_ context.Context, _ string, a emailaddress.Address) error {
	p.unsuppressed = append(p.unsuppressed, string(a))
	return nil
}

type fakeDNS struct {
	upserts map[string][]es.DNSRecord
	deleted map[string][]es.DNSRecord
	err     error
}

func (f *fakeDNS) UpsertRecords(_ context.Context, shared string, records []es.DNSRecord) error {
	if f.err != nil {
		return f.err
	}
	f.upserts[shared] = append(f.upserts[shared], records...)
	return nil
}

func (f *fakeDNS) DeleteRecords(_ context.Context, shared string, records []es.DNSRecord) error {
	if f.deleted == nil {
		f.deleted = map[string][]es.DNSRecord{}
	}
	f.deleted[shared] = append(f.deleted[shared], records...)
	return nil
}

type fakeDMARC struct {
	value string
	err   error
}

func (f *fakeDMARC) LookupDMARC(context.Context, string) (string, error) { return f.value, f.err }

type fakeRecords struct {
	mx  map[string][]string
	txt map[string][]string
	err error
}

func (f *fakeRecords) LookupMX(_ context.Context, name string) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.mx[name], nil
}

func (f *fakeRecords) LookupTXT(_ context.Context, name string) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.txt[name], nil
}

type fakeMonitor struct {
	stats map[string]es.DomainStats
	errs  map[string]error
}

func (f *fakeMonitor) Name() string { return "fake-monitor" }

func (f *fakeMonitor) SpamRate(_ context.Context, domain string, _ time.Time) (es.DomainStats, error) {
	if err := f.errs[domain]; err != nil {
		return es.DomainStats{}, err
	}
	s := f.stats[domain]
	s.Domain = domain
	return s, nil
}

type fakeDecoder struct {
	n   es.Notification
	err error
}

func (f fakeDecoder) Decode([]byte) (es.Notification, error) { return f.n, f.err }

type fakeAlerts struct {
	mu       sync.Mutex
	drafts   []notification.Draft
	resolved []string
}

func (f *fakeAlerts) Publish(_ context.Context, d notification.Draft) (notification.Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := d.Normalize().Validate(); err != nil {
		return notification.Record{}, err
	}
	f.drafts = append(f.drafts, d)
	return notification.Record{}, nil
}

func (f *fakeAlerts) Resolve(_ context.Context, _ string, keys ...string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resolved = append(f.resolved, keys...)
	return nil
}

func (f *fakeAlerts) keys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, d := range f.drafts {
		out = append(out, d.EpisodeKey+d.DedupKey)
	}
	return out
}

type fakeOps struct {
	mu     sync.Mutex
	alerts []string
}

func (f *fakeOps) Alert(_ context.Context, subject, detail string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.alerts = append(f.alerts, subject+": "+detail)
	return nil
}

type fakePacer struct {
	mu      sync.Mutex
	tickets []es.SendTicket
	err     error
}

func (f *fakePacer) Admit(_ context.Context, ticket es.SendTicket) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tickets = append(f.tickets, ticket)
	return f.err
}

type fakeQuota struct {
	mu    sync.Mutex
	quota es.SendQuota
	err   error
	lanes []es.ProviderLane
}

func (f *fakeQuota) ReadSendQuota(ctx context.Context) (es.SendQuota, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lanes = append(f.lanes, es.ProviderLaneOf(ctx))
	return f.quota, f.err
}

type fakeNames map[string]string

func (f fakeNames) WorkspaceName(_ context.Context, ws string) (string, error) {
	name, ok := f[ws]
	if !ok {
		return "", errors.New("unknown workspace")
	}
	return name, nil
}

func (f fakeNames) WorkspaceNamesByID(_ context.Context, ids []string) (map[string]string, error) {
	out := map[string]string{}
	for _, id := range ids {
		if name, ok := f[id]; ok {
			out[id] = name
		}
	}
	return out, nil
}

type queuedWake struct {
	mu  sync.Mutex
	ids []string
	err error
}

func (q *queuedWake) ScheduleFire(id string, _ time.Time) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.err != nil {
		return q.err
	}
	q.ids = append(q.ids, id)
	return nil
}

func (q *queuedWake) take() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	ids := q.ids
	q.ids = nil
	return ids
}

type harness struct {
	m            *Module
	wake         *queuedWake
	accounts     *memAccounts
	domains      *memDomains
	senders      *memSenders
	usage        *memUsage
	suppressions *memSuppressions
	messages     *memMessages
	provider     *fakeProvider
	dns          *fakeDNS
	dmarc        *fakeDMARC
	records      *fakeRecords
	monitor      *fakeMonitor
	alerts       *fakeAlerts
	ops          *fakeOps
	tenants      *fakeTenants
	activity     *fakeActivity
	live         *fakeLive
	locks        *memLocks
	routes       *fakeRoutes
	pauses       *memReplyPauses
	traffic      *fakeTraffic
	shared       *memSendState
	pacer        *fakePacer
	quota        *fakeQuota
	reputation   *fakeReputation
	hygiene      *fakeHygiene
	testBatches  *fakeTestBatches
	plans        *fakePlans
	unused       *memUnused
	codec        *emailtoken.Codec
	now          time.Time
}

const (
	wsID   = "11111111-1111-1111-1111-111111111111"
	wsName = "Loja da Joana"
)

func newHarness(t *testing.T) *harness {
	t.Helper()
	codec, err := emailtoken.NewCodec(strings.Repeat("s", 32))
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{
		accounts:     &memAccounts{rows: map[string]es.SenderAccount{}},
		domains:      &memDomains{rows: map[string]es.SendingDomain{}},
		senders:      &memSenders{rows: map[string]es.Sender{}},
		usage:        &memUsage{used: map[string]int64{}},
		suppressions: &memSuppressions{rows: map[string]es.Suppression{}},
		messages:     &memMessages{rows: map[string]es.Message{}, events: map[string]bool{}},
		provider:     &fakeProvider{states: map[string]es.IdentityState{}, health: es.TenantHealth{Sending: es.TenantEnabled}},
		dns:          &fakeDNS{upserts: map[string][]es.DNSRecord{}},
		dmarc:        &fakeDMARC{},
		records:      &fakeRecords{mx: map[string][]string{}, txt: map[string][]string{}},
		monitor:      &fakeMonitor{stats: map[string]es.DomainStats{}, errs: map[string]error{}},
		alerts:       &fakeAlerts{},
		ops:          &fakeOps{},
		tenants:      &fakeTenants{},
		activity:     &fakeActivity{lastSent: map[string]time.Time{}},
		live:         &fakeLive{live: map[string]bool{}},
		locks:        &memLocks{held: map[string]chan struct{}{}},
		routes:       &fakeRoutes{failPause: map[string]bool{}, failResume: map[string]bool{}},
		traffic:      &fakeTraffic{counts: map[string]int64{}},
		shared:       newMemSendState(),
		pacer:        &fakePacer{},
		quota:        &fakeQuota{quota: es.SendQuota{MaxSendRate: 14, Max24HourSend: 50000, ProductionAccess: true, Read: true}},
		reputation:   &fakeReputation{},
		hygiene:      &fakeHygiene{},
		testBatches:  &fakeTestBatches{},
		plans:        &fakePlans{refused: map[string]error{}},
		codec:        codec,
		wake:         &queuedWake{},
		now:          testNow,
	}
	h.pauses = &memReplyPauses{domains: h.domains, balances: map[string]int64{}}
	h.unused = &memUnused{accounts: h.accounts, domains: h.domains, senders: h.senders, used: map[string]bool{}, useOnForget: map[string]bool{}}
	m, err := New(h.deps())
	if err != nil {
		t.Fatal(err)
	}
	h.m = m
	return h
}

func (h *harness) deps() Deps {
	return Deps{
		Accounts:     h.accounts,
		Domains:      h.domains,
		Senders:      h.senders,
		Usage:        h.usage,
		Suppressions: h.suppressions,
		Messages:     h.messages,
		Provider:     h.provider,
		DNS:          h.dns,
		DMARC:        h.dmarc,
		Records:      h.records,
		Monitor:      h.monitor,
		Decoder:      fakeDecoder{},
		Alerts:       h.alerts,
		Ops:          h.ops,
		Tenants:      h.tenants,
		Activity:     h.activity,
		Campaigns:    h.live,
		Locks:        h.locks,
		Routes:       h.routes,
		ReplyPauses:  h.pauses,
		Traffic:      h.traffic,
		Shared:       h.shared,
		Pacer:        h.pacer,
		Quota:        h.quota,
		Workspaces:   fakeNames{wsID: wsName, "22222222-2222-2222-2222-222222222222": "Outra Loja"},
		Codec:        h.codec,
		SetupWake:    h.wake,
		Reputation:   h.reputation,
		Hygiene:      h.hygiene,
		TestBatches:  h.testBatches,
		Plans:        h.plans,
		Unused:       h.unused,
		Config:       Config{ProbationDomain: "envios.test", TrustedDomain: "mail.test", PublicBaseURL: "https://api.test/"},
		Now:          func() time.Time { return h.now },
	}
}

func (h *harness) runSetup() error {
	var errs []error
	for _, ws := range h.wake.take() {
		errs = append(errs, h.m.Setup.Complete(context.Background(), ws))
	}
	return errors.Join(errs...)
}

func (h *harness) provision(t *testing.T, ws string) StatusView {
	t.Helper()
	ctx := context.Background()
	if _, err := h.m.Setup.Provision(ctx, ws, ""); err != nil {
		t.Fatal(err)
	}
	if err := h.runSetup(); err != nil {
		t.Fatal(err)
	}
	view, err := h.m.Setup.Status(ctx, ws)
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func (h *harness) connect(t *testing.T, ws, domain string) *es.SendingDomain {
	t.Helper()
	ctx := context.Background()
	d, err := h.m.Domains.Connect(ctx, ws, domain)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.runSetup(); err != nil {
		t.Fatal(err)
	}
	stored, err := h.domains.Get(ctx, ws, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	return stored
}

func (h *harness) provisionVerified(t *testing.T, ws string) *es.SendingDomain {
	t.Helper()
	ctx := context.Background()
	h.provision(t, ws)
	d, err := h.m.c().domainOfKind(ctx, ws, es.DomainSharedProbation)
	if err != nil {
		t.Fatal(err)
	}
	h.provider.states[d.Domain] = es.IdentityState{DKIMVerified: true, MailFromVerified: true}
	if _, err := h.m.Domains.Recheck(ctx, ws, d.ID); err != nil {
		t.Fatal(err)
	}
	d, _ = h.domains.Get(ctx, ws, d.ID)
	return d
}

func (m *Module) c() *core { return m.Setup.c }

type fakeHygiene struct {
	mu      sync.Mutex
	kinds   []es.RunKind
	refusal error
}

func (f *fakeHygiene) SendRefusal(_ context.Context, _ string, kind es.RunKind, _ emailaddress.Address) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.kinds = append(f.kinds, kind)
	return f.refusal
}

type fakeReputation struct {
	mu     sync.Mutex
	seen   []es.EventKind
	answer es.SuppressReason
	err    error
}

func (f *fakeReputation) Record(_ context.Context, _ es.Message, ev es.DeliveryEvent, _ es.EventEffect) (es.SuppressReason, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seen = append(f.seen, ev.Kind)
	if f.err != nil {
		return "", f.err
	}
	if ev.Kind == es.EventBounce {
		return f.answer, nil
	}
	return "", nil
}

func (f *fakeReputation) kinds() []es.EventKind {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]es.EventKind(nil), f.seen...)
}

type fakeTestBatches struct {
	failed int
	err    error
}

func (f *fakeTestBatches) FailedTestBatches(context.Context, string, time.Time) (int, error) {
	return f.failed, f.err
}
