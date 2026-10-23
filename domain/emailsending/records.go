package emailsending

import "strings"

type RecordPurpose string

const (
	RecordDKIM        RecordPurpose = "dkim"
	RecordMailFromMX  RecordPurpose = "mail_from_mx"
	RecordMailFromSPF RecordPurpose = "mail_from_spf"
	RecordReplyMX     RecordPurpose = "reply_mx"
	RecordDMARC       RecordPurpose = "dmarc"
)

const (
	replyMXPriority = 10
	defaultDMARC    = "v=DMARC1; p=none;"
)

type DNSRecord struct {
	Purpose  RecordPurpose `json:"purpose"`
	Type     string        `json:"type"`
	Name     string        `json:"name"`
	Value    string        `json:"value"`
	Priority int           `json:"priority,omitempty"`
	TTL      int           `json:"ttl,omitempty"`
	Status   RecordStatus  `json:"status,omitempty"`
}

type IdentitySetup struct {
	Records     []DNSRecord
	InboundHost string
}

type DomainLayout struct {
	Kind           DomainKind
	Domain         string
	MailFromDomain string
	ReplyDomain    string
}

type DMARCState struct {
	Existing string
}

func LayoutFor(kind DomainKind, domain string) (DomainLayout, error) {
	if !kind.Valid() {
		return DomainLayout{}, ErrUnknownDomainKind
	}
	layout := DomainLayout{Kind: kind, Domain: domain, MailFromDomain: "bounce." + domain}
	if kind.Shared() {
		layout.ReplyDomain = domain
		return layout, nil
	}
	layout.ReplyDomain = "reply." + domain
	return layout, nil
}

func RequiredRecords(layout DomainLayout, setup IdentitySetup, dmarc DMARCState) []DNSRecord {
	records := make([]DNSRecord, 0, len(setup.Records)+2)
	records = append(records, setup.Records...)
	records = append(records, DNSRecord{
		Purpose:  RecordReplyMX,
		Type:     "MX",
		Name:     layout.ReplyDomain,
		Value:    setup.InboundHost,
		Priority: replyMXPriority,
	})
	if layout.Kind == DomainOwn && strings.TrimSpace(dmarc.Existing) == "" {
		records = append(records, DNSRecord{
			Purpose: RecordDMARC,
			Type:    "TXT",
			Name:    "_dmarc." + layout.Domain,
			Value:   defaultDMARC,
		})
	}
	return records
}

func DMARCCompatible(value string) bool {
	tags := map[string]string{}
	for _, part := range strings.Split(value, ";") {
		key, val, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		tags[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(val)
	}
	if !strings.EqualFold(tags["v"], "DMARC1") {
		return false
	}
	switch strings.ToLower(tags["p"]) {
	case "none", "quarantine", "reject":
		return true
	}
	return false
}

func RelativeHost(fqdn, domain string) string {
	if fqdn == domain {
		return "@"
	}
	if strings.HasSuffix(fqdn, "."+domain) {
		return strings.TrimSuffix(fqdn, "."+domain)
	}
	return fqdn
}
