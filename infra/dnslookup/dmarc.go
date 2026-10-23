package dnslookup

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"vozko/domain/emailaddress"
	"vozko/domain/emailsending"
)

type TXTResolver interface {
	LookupTXT(ctx context.Context, name string) ([]string, error)
}

type DMARC struct {
	resolver TXTResolver
}

var _ emailsending.DMARCLookup = (*DMARC)(nil)

func New(resolver TXTResolver) *DMARC {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	return &DMARC{resolver: resolver}
}

func (d *DMARC) LookupDMARC(ctx context.Context, domain string) (string, error) {
	normalized, err := emailaddress.NormalizeDomain(domain)
	if err != nil {
		return "", err
	}
	records, err := d.resolver.LookupTXT(ctx, "_dmarc."+normalized)
	if err != nil {
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			return "", nil
		}
		return "", fmt.Errorf("dnslookup: dmarc of %s: %w", normalized, err)
	}
	for _, r := range records {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(r)), "v=dmarc1") {
			return strings.TrimSpace(r), nil
		}
	}
	return "", nil
}
