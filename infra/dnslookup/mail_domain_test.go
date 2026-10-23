package dnslookup

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"vozko/domain/emailhygiene"
)

type fakeMailResolver struct {
	mx      []*net.MX
	mxErr   error
	ips     []net.IPAddr
	ipErr   error
	ipCalls int
	wait    time.Duration
	names   []string
}

func (f *fakeMailResolver) LookupMX(ctx context.Context, name string) ([]*net.MX, error) {
	f.names = append(f.names, name)
	if f.wait > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(f.wait):
		}
	}
	return f.mx, f.mxErr
}

func (f *fakeMailResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	f.names = append(f.names, host)
	f.ipCalls++
	return f.ips, f.ipErr
}

func notFound() error { return &net.DNSError{Err: "no such host", IsNotFound: true} }

func TestMailDomainsTranslatesResolverAnswers(t *testing.T) {
	cases := []struct {
		name     string
		resolver *fakeMailResolver
		want     emailhygiene.DomainVerdict
		ipLookup bool
	}{
		{name: "mx hosts accept mail without an address lookup", resolver: &fakeMailResolver{mx: []*net.MX{{Host: "mx.loja.com.br.", Pref: 10}}},
			want: emailhygiene.DomainVerdict{State: emailhygiene.DomainAccepts, Detail: emailhygiene.DetailMX}},
		{name: "a null mx refuses mail", resolver: &fakeMailResolver{mx: []*net.MX{{Host: ".", Pref: 0}}},
			want: emailhygiene.DomainVerdict{State: emailhygiene.DomainNoMail, Detail: emailhygiene.DetailNullMX}},
		{name: "no mx falls back to the address records", resolver: &fakeMailResolver{mxErr: notFound(), ips: []net.IPAddr{{IP: net.IPv4(1, 2, 3, 4)}}},
			want: emailhygiene.DomainVerdict{State: emailhygiene.DomainAccepts, Detail: emailhygiene.DetailImplicitMX}, ipLookup: true},
		{name: "no mx and no address is an empty answer, unknown until repeated", resolver: &fakeMailResolver{mxErr: notFound(), ipErr: notFound()},
			want: emailhygiene.DomainVerdict{State: emailhygiene.DomainUnknown, Detail: emailhygiene.DetailNoRecords}, ipLookup: true},
		{name: "an empty mx answer is treated as no mx", resolver: &fakeMailResolver{ips: []net.IPAddr{{IP: net.IPv4(1, 2, 3, 4)}}},
			want: emailhygiene.DomainVerdict{State: emailhygiene.DomainAccepts, Detail: emailhygiene.DetailImplicitMX}, ipLookup: true},
		{name: "a resolver timeout is unknown", resolver: &fakeMailResolver{mxErr: &net.DNSError{Err: "i/o timeout", IsTimeout: true}},
			want: emailhygiene.DomainVerdict{State: emailhygiene.DomainUnknown, Detail: emailhygiene.DetailTimeout}},
		{name: "a server failure is unknown", resolver: &fakeMailResolver{mxErr: &net.DNSError{Err: "server misbehaving", IsTemporary: true}},
			want: emailhygiene.DomainVerdict{State: emailhygiene.DomainUnknown, Detail: emailhygiene.DetailError}},
		{name: "a malformed answer is unknown", resolver: &fakeMailResolver{mxErr: errors.New("cannot unmarshal DNS message")},
			want: emailhygiene.DomainVerdict{State: emailhygiene.DomainUnknown, Detail: emailhygiene.DetailError}},
		{name: "an address timeout after no mx is unknown", resolver: &fakeMailResolver{mxErr: notFound(), ipErr: &net.DNSError{Err: "i/o timeout", IsTimeout: true}},
			want: emailhygiene.DomainVerdict{State: emailhygiene.DomainUnknown, Detail: emailhygiene.DetailTimeout}, ipLookup: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := emailhygiene.Classify(NewMailDomains(tc.resolver, time.Second).Resolve(context.Background(), "loja.com.br"))
			if got != tc.want {
				t.Fatalf("verdict = %+v, want %+v", got, tc.want)
			}
			if (tc.resolver.ipCalls > 0) != tc.ipLookup {
				t.Fatalf("address lookups = %d, want lookup %v", tc.resolver.ipCalls, tc.ipLookup)
			}
		})
	}
}

func TestMailDomainsBoundsEveryLookupByItsTimeout(t *testing.T) {
	resolver := &fakeMailResolver{wait: time.Second, mx: []*net.MX{{Host: "mx.loja.com.br."}}}
	started := time.Now()
	got := emailhygiene.Classify(NewMailDomains(resolver, 20*time.Millisecond).Resolve(context.Background(), "loja.com.br"))
	if got.State != emailhygiene.DomainUnknown || got.Detail != emailhygiene.DetailTimeout {
		t.Fatalf("verdict = %+v, a lookup past its deadline is a timeout", got)
	}
	if time.Since(started) > 500*time.Millisecond {
		t.Fatal("the lookup did not respect its timeout")
	}
}

func TestMailDomainsRefusesAnInvalidName(t *testing.T) {
	got := emailhygiene.Classify(NewMailDomains(&fakeMailResolver{}, time.Second).Resolve(context.Background(), "not a domain"))
	if got.State != emailhygiene.DomainUnknown {
		t.Fatalf("verdict = %+v, an unusable name is never looked up and never valid", got)
	}
}

func TestMailDomainsNeverReadsAnErrorAsAMissingDomain(t *testing.T) {
	refused := &net.OpError{Op: "dial", Net: "udp", Err: errors.New("connection refused")}
	cases := []struct {
		name     string
		resolver *fakeMailResolver
		want     emailhygiene.DomainVerdict
	}{
		{name: "an mx timeout", resolver: &fakeMailResolver{mxErr: &net.DNSError{Err: "i/o timeout", IsTimeout: true, IsTemporary: true}},
			want: emailhygiene.DomainVerdict{State: emailhygiene.DomainUnknown, Detail: emailhygiene.DetailTimeout}},
		{name: "an mx deadline", resolver: &fakeMailResolver{mxErr: context.DeadlineExceeded},
			want: emailhygiene.DomainVerdict{State: emailhygiene.DomainUnknown, Detail: emailhygiene.DetailTimeout}},
		{name: "an mx servfail", resolver: &fakeMailResolver{mxErr: &net.DNSError{Err: "server misbehaving", IsTemporary: true}},
			want: emailhygiene.DomainVerdict{State: emailhygiene.DomainUnknown, Detail: emailhygiene.DetailError}},
		{name: "an mx lame referral", resolver: &fakeMailResolver{mxErr: &net.DNSError{Err: "lame referral"}},
			want: emailhygiene.DomainVerdict{State: emailhygiene.DomainUnknown, Detail: emailhygiene.DetailError}},
		{name: "an mx with no answer from the server", resolver: &fakeMailResolver{mxErr: &net.DNSError{Err: "no answer from DNS server"}},
			want: emailhygiene.DomainVerdict{State: emailhygiene.DomainUnknown, Detail: emailhygiene.DetailError}},
		{name: "an unreachable resolver", resolver: &fakeMailResolver{mxErr: refused},
			want: emailhygiene.DomainVerdict{State: emailhygiene.DomainUnknown, Detail: emailhygiene.DetailError}},
		{name: "a canceled lookup", resolver: &fakeMailResolver{mxErr: context.Canceled},
			want: emailhygiene.DomainVerdict{State: emailhygiene.DomainUnknown, Detail: emailhygiene.DetailError}},
		{name: "malformed mx hosts next to an error", resolver: &fakeMailResolver{mx: []*net.MX{{Host: "mx.loja.com.br."}}, mxErr: &net.DNSError{Err: "DNS response contained records which contain invalid names"}},
			want: emailhygiene.DomainVerdict{State: emailhygiene.DomainUnknown, Detail: emailhygiene.DetailError}},
		{name: "no mx then an address servfail", resolver: &fakeMailResolver{mxErr: notFound(), ipErr: &net.DNSError{Err: "server misbehaving", IsTemporary: true}},
			want: emailhygiene.DomainVerdict{State: emailhygiene.DomainUnknown, Detail: emailhygiene.DetailError}},
		{name: "no mx then an unreachable resolver", resolver: &fakeMailResolver{mxErr: notFound(), ipErr: refused},
			want: emailhygiene.DomainVerdict{State: emailhygiene.DomainUnknown, Detail: emailhygiene.DetailError}},
		{name: "no mx then an address deadline", resolver: &fakeMailResolver{mxErr: notFound(), ipErr: context.DeadlineExceeded},
			want: emailhygiene.DomainVerdict{State: emailhygiene.DomainUnknown, Detail: emailhygiene.DetailTimeout}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := emailhygiene.Classify(NewMailDomains(tc.resolver, time.Second).Resolve(context.Background(), "gmail.com"))
			if got != tc.want {
				t.Fatalf("verdict = %+v, want %+v", got, tc.want)
			}
			if got.State == emailhygiene.DomainNoMail {
				t.Fatal("an error must never refuse mail")
			}
		})
	}
}

func TestMailDomainsQueriesTheRootedNameSoTheSearchListCannotAnswer(t *testing.T) {
	resolver := &fakeMailResolver{mxErr: notFound(), ips: []net.IPAddr{{IP: net.IPv4(1, 2, 3, 4)}}}
	NewMailDomains(resolver, time.Second).Resolve(context.Background(), "Gmail.COM")
	if len(resolver.names) != 2 {
		t.Fatalf("names = %v, want one mx and one address lookup", resolver.names)
	}
	for _, name := range resolver.names {
		if name != "gmail.com." {
			t.Fatalf("looked up %q; an unrooted name walks the container search list and a suffix NXDOMAIN hides the real error", name)
		}
	}
}
