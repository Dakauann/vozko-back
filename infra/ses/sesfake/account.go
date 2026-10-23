package sesfake

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"vozko/domain/emailaddress"
	es "vozko/domain/emailsending"
)

const (
	ProviderName = "ses"
	Region       = "sa-east-1"
	AccountID    = "000000000000"
)

var (
	ErrTenantMissing   = errors.New("sesfake: the tenant does not exist")
	ErrRawMissing      = errors.New("sesfake: no raw message under that key")
	ErrIdentityMissing = errors.New("sesfake: the identity does not exist")
)

type Sent struct {
	Message           es.OutboundMessage
	ProviderMessageID string
	At                time.Time
}

type Account struct {
	mu           sync.Mutex
	tenants      map[string]bool
	identities   map[string]string
	verified     map[string]bool
	sent         []Sent
	refusals     []error
	conditions   map[string]map[string]bool
	filters      map[netip.Addr]time.Time
	raw          map[string][]byte
	deletedRaw   []string
	quota        es.SendQuota
	retired      int
	revived      int
	reviveDelay  time.Duration
	tenantMisses int
}

var (
	_ es.Provider        = (*Account)(nil)
	_ es.TenantLifecycle = (*Account)(nil)
	_ es.InboundRoutes   = (*Account)(nil)
	_ es.SendQuotaReader = (*Account)(nil)
	_ es.RawMessageStore = (*Account)(nil)
)

func New() *Account {
	return &Account{
		tenants:    map[string]bool{},
		identities: map[string]string{},
		verified:   map[string]bool{},
		conditions: map[string]map[string]bool{},
		filters:    map[netip.Addr]time.Time{},
		raw:        map[string][]byte{},
		quota:      UnlimitedQuota(),
	}
}

func UnlimitedQuota() es.SendQuota {
	return es.SendQuota{MaxSendRate: 100000, Max24HourSend: -1, ProductionAccess: true}
}

func TenantRef(name string) string {
	return "arn:aws:ses:" + Region + ":" + AccountID + ":tenant/" + name
}

func (a *Account) Name() string { return ProviderName }

func (a *Account) EnsureTenant(_ context.Context, spec es.TenantSpec) (es.Tenant, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tenants[spec.Name] = true
	return es.Tenant{Name: spec.Name, Ref: TenantRef(spec.Name)}, nil
}

func (a *Account) EnsureIdentity(_ context.Context, spec es.IdentitySpec) (es.IdentitySetup, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.tenants[spec.TenantName] {
		return es.IdentitySetup{}, ErrTenantMissing
	}
	a.identities[spec.Domain] = spec.TenantName
	return es.IdentitySetup{
		Records: []es.DNSRecord{
			{Purpose: es.RecordDKIM, Type: "CNAME", Name: "k1._domainkey." + spec.Domain, Value: "k1.dkim.amazonses.com"},
			{Purpose: es.RecordDKIM, Type: "CNAME", Name: "k2._domainkey." + spec.Domain, Value: "k2.dkim.amazonses.com"},
			{Purpose: es.RecordMailFromMX, Type: "MX", Name: spec.MailFromDomain, Value: "feedback-smtp." + Region + ".amazonses.com", Priority: 10},
			{Purpose: es.RecordMailFromSPF, Type: "TXT", Name: spec.MailFromDomain, Value: "v=spf1 include:amazonses.com ~all"},
		},
		InboundHost: "inbound-smtp." + Region + ".amazonaws.com",
	}, nil
}

func (a *Account) PublishDNS() {
	a.mu.Lock()
	defer a.mu.Unlock()
	for domain := range a.identities {
		a.verified[domain] = true
	}
}

func (a *Account) IdentityStatus(_ context.Context, domain string) (es.IdentityState, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.identities[domain]; !ok {
		return es.IdentityState{}, ErrIdentityMissing
	}
	ok := a.verified[domain]
	return es.IdentityState{DKIMVerified: ok, MailFromVerified: ok}, nil
}

func (a *Account) RemoveIdentity(_ context.Context, _, domain string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.identities, domain)
	delete(a.verified, domain)
	return nil
}

func (a *Account) RefuseNextSends(errs ...error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.refusals = append(a.refusals, errs...)
}

func (a *Account) Send(_ context.Context, msg es.OutboundMessage) (es.SendReceipt, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := msg.Validate(); err != nil {
		return es.SendReceipt{}, &es.SendError{Kind: es.SendErrorRejected, Detail: err.Error()}
	}
	if !a.tenants[msg.TenantName] {
		a.tenantMisses++
		return es.SendReceipt{}, &es.SendError{Kind: es.SendErrorConfiguration, Detail: ErrTenantMissing.Error()}
	}
	if len(a.refusals) > 0 {
		err := a.refusals[0]
		a.refusals = a.refusals[1:]
		return es.SendReceipt{}, err
	}
	id := "0100" + strings.ReplaceAll(uuid.NewString(), "-", "") + "-000000"
	a.sent = append(a.sent, Sent{Message: msg, ProviderMessageID: id, At: time.Now().UTC()})
	return es.SendReceipt{ProviderMessageID: id}, nil
}

func (a *Account) SetTenantSending(_ context.Context, tenantName string, _ bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.tenants[tenantName] {
		return ErrTenantMissing
	}
	return nil
}

func (a *Account) TenantHealth(_ context.Context, tenantName string) (es.TenantHealth, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.tenants[tenantName] {
		return es.TenantHealth{}, ErrTenantMissing
	}
	return es.TenantHealth{Sending: es.TenantEnabled}, nil
}

func (a *Account) SuppressAddress(context.Context, string, emailaddress.Address, es.SuppressReason) error {
	return nil
}

func (a *Account) UnsuppressAddress(context.Context, string, emailaddress.Address) error {
	return nil
}

func (a *Account) SlowRevive(d time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.reviveDelay = d
}

func (a *Account) RetireTenant(_ context.Context, tenant es.TenantResources) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.tenants, tenant.Name)
	a.retired++
	return nil
}

func (a *Account) ReviveTenant(_ context.Context, tenant es.TenantResources) (es.Tenant, error) {
	a.mu.Lock()
	delay := a.reviveDelay
	a.mu.Unlock()
	time.Sleep(delay)
	a.mu.Lock()
	defer a.mu.Unlock()
	a.tenants[tenant.Name] = true
	for _, domain := range tenant.Identities {
		a.identities[domain] = tenant.Name
	}
	a.revived++
	return es.Tenant{Name: tenant.Name, Ref: TenantRef(tenant.Name)}, nil
}

func (a *Account) EnsureReplyRoute(_ context.Context, replyDomain string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.conditions[replyDomain] = map[string]bool{}
	for _, recipient := range es.InboundRecipients(replyDomain) {
		a.conditions[replyDomain][recipient] = true
	}
	return nil
}

func (a *Account) PauseReplies(_ context.Context, replyDomain string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.conditions[replyDomain], es.ReplyRecipient(replyDomain))
	return nil
}

func (a *Account) ResumeReplies(_ context.Context, replyDomain string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.conditions[replyDomain] == nil {
		a.conditions[replyDomain] = map[string]bool{}
	}
	a.conditions[replyDomain][es.ReplyRecipient(replyDomain)] = true
	return nil
}

func (a *Account) RemoveReplyRoute(_ context.Context, replyDomain string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.conditions, replyDomain)
	return nil
}

func (a *Account) AdoptLegacyRoutes(_ context.Context, routes []es.ReplyRoute) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for _, r := range routes {
		if a.conditions[r.ReplyDomain] == nil {
			a.conditions[r.ReplyDomain] = map[string]bool{}
		}
		for _, recipient := range es.InboundRecipients(r.ReplyDomain) {
			a.conditions[r.ReplyDomain][recipient] = true
		}
		if r.Paused {
			delete(a.conditions[r.ReplyDomain], es.ReplyRecipient(r.ReplyDomain))
		}
	}
	return nil
}

func (a *Account) BlockSender(_ context.Context, ip netip.Addr, until time.Time) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.filters[ip]; ok {
		return false, nil
	}
	a.filters[ip] = until
	return true, nil
}

func (a *Account) UnblockExpired(_ context.Context, now time.Time) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	for ip, until := range a.filters {
		if !until.After(now) {
			delete(a.filters, ip)
		}
	}
	return nil
}

func (a *Account) IPFilterCount(context.Context) (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.filters), nil
}

func (a *Account) SetQuota(q es.SendQuota) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.quota = q
}

func (a *Account) ReadSendQuota(context.Context) (es.SendQuota, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.quota, nil
}

func (a *Account) Fetch(_ context.Context, objectKey string) ([]byte, error) {
	if !es.ValidRawMessageKey(objectKey) {
		return nil, es.ErrRawMessageKey
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	raw, ok := a.raw[objectKey]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrRawMissing, objectKey)
	}
	return append([]byte(nil), raw...), nil
}

func (a *Account) Delete(_ context.Context, objectKey string) error {
	if !es.ValidRawMessageKey(objectKey) {
		return es.ErrRawMessageKey
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.raw, objectKey)
	a.deletedRaw = append(a.deletedRaw, objectKey)
	return nil
}

func (a *Account) Sent() []Sent {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]Sent(nil), a.sent...)
}

func (a *Account) SentTo(address emailaddress.Address) []Sent {
	var out []Sent
	for _, s := range a.Sent() {
		if s.Message.To == address {
			out = append(out, s)
		}
	}
	return out
}

func (a *Account) HasIdentity(domain string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, ok := a.identities[domain]
	return ok
}

func (a *Account) HasTenant(name string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.tenants[name]
}

func (a *Account) Retirements() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.retired
}

func (a *Account) Revivals() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.revived
}

func (a *Account) SendsToMissingTenants() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.tenantMisses
}

func (a *Account) Receiving(replyDomain string) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []string
	for recipient := range a.conditions[replyDomain] {
		out = append(out, recipient)
	}
	sort.Strings(out)
	return out
}

func (a *Account) Blocked(ip netip.Addr) (time.Time, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	until, ok := a.filters[ip]
	return until, ok
}

func (a *Account) HasRaw(objectKey string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	_, ok := a.raw[objectKey]
	return ok
}

func (a *Account) DeletedRaw() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.deletedRaw...)
}

func (a *Account) storeRaw(objectKey string, raw []byte) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.raw[objectKey] = raw
}
