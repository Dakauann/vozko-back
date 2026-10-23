package emailsending

import (
	"testing"
)

func providerSetup() IdentitySetup {
	return IdentitySetup{
		Records: []DNSRecord{
			{Purpose: RecordDKIM, Type: "CNAME", Name: "abc._domainkey.loja.com.br", Value: "abc.dkim.provider.test"},
			{Purpose: RecordDKIM, Type: "CNAME", Name: "def._domainkey.loja.com.br", Value: "def.dkim.provider.test"},
			{Purpose: RecordDKIM, Type: "CNAME", Name: "ghi._domainkey.loja.com.br", Value: "ghi.dkim.provider.test"},
			{Purpose: RecordMailFromMX, Type: "MX", Name: "bounce.loja.com.br", Value: "feedback.provider.test", Priority: 10},
			{Purpose: RecordMailFromSPF, Type: "TXT", Name: "bounce.loja.com.br", Value: "v=spf1 include:provider.test ~all"},
		},
		InboundHost: "inbound.provider.test",
	}
}

func TestOwnDomainLayout(t *testing.T) {
	layout, err := LayoutFor(DomainOwn, "loja.com.br")
	if err != nil {
		t.Fatal(err)
	}
	if layout.MailFromDomain != "bounce.loja.com.br" || layout.ReplyDomain != "reply.loja.com.br" {
		t.Fatalf("layout = %+v", layout)
	}
	shared, err := LayoutFor(DomainSharedTrusted, "joao.vozkomail.com.br")
	if err != nil {
		t.Fatal(err)
	}
	if shared.MailFromDomain != "bounce.joao.vozkomail.com.br" || shared.ReplyDomain != "joao.vozkomail.com.br" {
		t.Fatalf("shared layout = %+v", shared)
	}
	if _, err := LayoutFor("other", "loja.com.br"); err == nil {
		t.Fatal("unknown kind must fail")
	}
}

func TestRequiredRecordsOwnDomainWithoutDMARC(t *testing.T) {
	layout, _ := LayoutFor(DomainOwn, "loja.com.br")
	records := RequiredRecords(layout, providerSetup(), DMARCState{})
	counts := map[RecordPurpose]int{}
	for _, r := range records {
		counts[r.Purpose]++
	}
	if counts[RecordDKIM] != 3 || counts[RecordMailFromMX] != 1 || counts[RecordMailFromSPF] != 1 || counts[RecordReplyMX] != 1 || counts[RecordDMARC] != 1 {
		t.Fatalf("counts = %v", counts)
	}
	for _, r := range records {
		if r.Purpose == RecordReplyMX && (r.Name != "reply.loja.com.br" || r.Value != "inbound.provider.test" || r.Priority != 10) {
			t.Fatalf("reply mx = %+v", r)
		}
		if r.Purpose == RecordDMARC && (r.Name != "_dmarc.loja.com.br" || r.Type != "TXT" || r.Value != "v=DMARC1; p=none;") {
			t.Fatalf("dmarc = %+v", r)
		}
	}
}

func TestRequiredRecordsKeepExistingDMARC(t *testing.T) {
	layout, _ := LayoutFor(DomainOwn, "loja.com.br")
	records := RequiredRecords(layout, providerSetup(), DMARCState{Existing: "v=DMARC1; p=quarantine"})
	for _, r := range records {
		if r.Purpose == RecordDMARC {
			t.Fatalf("existing DMARC must be kept, got %+v", r)
		}
	}
}

func TestRequiredRecordsSharedSubdomainHasNoDMARC(t *testing.T) {
	layout, _ := LayoutFor(DomainSharedProbation, "joao.vozkoenvios.com.br")
	setup := providerSetup()
	records := RequiredRecords(layout, setup, DMARCState{})
	for _, r := range records {
		if r.Purpose == RecordDMARC {
			t.Fatal("shared subdomains inherit the organisational DMARC of the shared domain")
		}
		if r.Purpose == RecordReplyMX && r.Name != "joao.vozkoenvios.com.br" {
			t.Fatalf("shared reply mx on the subdomain itself, got %+v", r)
		}
	}
}

func TestDMARCCompatible(t *testing.T) {
	cases := []struct {
		value string
		want  bool
	}{
		{value: "v=DMARC1; p=none", want: true},
		{value: "v=DMARC1;p=reject;adkim=r", want: true},
		{value: "v=DMARC1; p=quarantine; adkim=s", want: true},
		{value: "v=spf1 include:x", want: false},
		{value: "", want: false},
		{value: "v=DMARC1", want: false},
	}
	for _, tc := range cases {
		if got := DMARCCompatible(tc.value); got != tc.want {
			t.Fatalf("DMARCCompatible(%q) = %v, want %v", tc.value, got, tc.want)
		}
	}
}

func TestRelativeHost(t *testing.T) {
	cases := []struct {
		fqdn, domain, want string
	}{
		{fqdn: "loja.com.br", domain: "loja.com.br", want: "@"},
		{fqdn: "bounce.loja.com.br", domain: "loja.com.br", want: "bounce"},
		{fqdn: "abc._domainkey.loja.com.br", domain: "loja.com.br", want: "abc._domainkey"},
		{fqdn: "other.com", domain: "loja.com.br", want: "other.com"},
	}
	for _, tc := range cases {
		if got := RelativeHost(tc.fqdn, tc.domain); got != tc.want {
			t.Fatalf("RelativeHost(%q, %q) = %q, want %q", tc.fqdn, tc.domain, got, tc.want)
		}
	}
}
