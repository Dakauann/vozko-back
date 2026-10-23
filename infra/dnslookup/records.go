package dnslookup

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"

	"vozko/domain/emailsending"
)

var errEmptyName = errors.New("dnslookup: the name to look up is empty")

type RecordResolver interface {
	TXTResolver
	LookupMX(ctx context.Context, name string) ([]*net.MX, error)
}

type Records struct {
	resolver RecordResolver
}

var _ emailsending.DNSRecordLookup = (*Records)(nil)

func NewRecords(resolver RecordResolver) *Records {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	return &Records{resolver: resolver}
}

func (r *Records) LookupMX(ctx context.Context, name string) ([]string, error) {
	host, err := lookupName(name)
	if err != nil {
		return nil, err
	}
	records, err := r.resolver.LookupMX(ctx, host)
	if err != nil {
		return nil, absentOr(err, "mx", host)
	}
	sort.SliceStable(records, func(i, j int) bool { return records[i].Pref < records[j].Pref })
	out := make([]string, 0, len(records))
	for _, mx := range records {
		out = append(out, strings.ToLower(strings.TrimSuffix(mx.Host, ".")))
	}
	return out, nil
}

func (r *Records) LookupTXT(ctx context.Context, name string) ([]string, error) {
	host, err := lookupName(name)
	if err != nil {
		return nil, err
	}
	records, err := r.resolver.LookupTXT(ctx, host)
	if err != nil {
		return nil, absentOr(err, "txt", host)
	}
	out := make([]string, 0, len(records))
	for _, v := range records {
		out = append(out, strings.TrimSpace(v))
	}
	return out, nil
}

func lookupName(name string) (string, error) {
	host := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
	if host == "" {
		return "", errEmptyName
	}
	return host, nil
}

func absentOr(err error, kind, host string) error {
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
		return nil
	}
	return fmt.Errorf("dnslookup: %s of %s: %w", kind, host, err)
}
