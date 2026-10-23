package dnslookup

import (
	"context"
	"errors"
	"net"
	"time"

	"vozko/domain/emailaddress"
	"vozko/domain/emailhygiene"
)

type MailResolver interface {
	LookupMX(ctx context.Context, name string) ([]*net.MX, error)
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

type MailDomains struct {
	resolver MailResolver
	timeout  time.Duration
}

var _ emailhygiene.MailDomainResolver = (*MailDomains)(nil)

func NewMailDomains(resolver MailResolver, timeout time.Duration) *MailDomains {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	if timeout <= 0 {
		timeout = emailhygiene.LookupTimeout
	}
	return &MailDomains{resolver: resolver, timeout: timeout}
}

func (m *MailDomains) Resolve(ctx context.Context, domain string) emailhygiene.DNSAnswer {
	normalized, err := emailaddress.NormalizeDomain(domain)
	if err != nil {
		return emailhygiene.DNSAnswer{MX: emailhygiene.LookupFailed}
	}
	name := normalized + "."
	mxCtx, cancel := context.WithTimeout(ctx, m.timeout)
	records, err := m.resolver.LookupMX(mxCtx, name)
	cancel()
	if err != nil {
		outcome := outcomeOf(err)
		if outcome != emailhygiene.LookupAbsent {
			return emailhygiene.DNSAnswer{MX: outcome}
		}
	}
	if err == nil && len(records) > 0 {
		hosts := make([]string, 0, len(records))
		for _, r := range records {
			if r != nil {
				hosts = append(hosts, r.Host)
			}
		}
		return emailhygiene.DNSAnswer{MX: emailhygiene.LookupFound, MXHosts: hosts}
	}
	ipCtx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()
	ips, err := m.resolver.LookupIPAddr(ipCtx, name)
	switch {
	case err != nil:
		return emailhygiene.DNSAnswer{MX: emailhygiene.LookupAbsent, Address: outcomeOf(err)}
	case len(ips) == 0:
		return emailhygiene.DNSAnswer{MX: emailhygiene.LookupAbsent, Address: emailhygiene.LookupAbsent}
	}
	return emailhygiene.DNSAnswer{MX: emailhygiene.LookupAbsent, Address: emailhygiene.LookupFound}
}

func outcomeOf(err error) emailhygiene.LookupOutcome {
	var dnsErr *net.DNSError
	switch {
	case errors.As(err, &dnsErr) && dnsErr.IsNotFound:
		return emailhygiene.LookupAbsent
	case errors.As(err, &dnsErr) && dnsErr.IsTimeout, errors.Is(err, context.DeadlineExceeded):
		return emailhygiene.LookupTimedOut
	}
	return emailhygiene.LookupFailed
}
