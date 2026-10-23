package emailsending

import "testing"

func checkedRecords() []DNSRecord {
	return []DNSRecord{
		{Purpose: RecordDKIM, Type: "CNAME", Name: "abc._domainkey.loja.com.br", Value: "abc.dkim.provider.test"},
		{Purpose: RecordMailFromMX, Type: "MX", Name: "bounce.loja.com.br", Value: "feedback.provider.test", Priority: 10},
		{Purpose: RecordMailFromSPF, Type: "TXT", Name: "bounce.loja.com.br", Value: "v=spf1 include:provider.test ~all"},
		{Purpose: RecordReplyMX, Type: "MX", Name: "reply.loja.com.br", Value: "inbound.provider.test", Priority: 10},
		{Purpose: RecordDMARC, Type: "TXT", Name: "_dmarc.loja.com.br", Value: "v=DMARC1; p=none;"},
	}
}

func statusesOf(records []DNSRecord) map[RecordPurpose]RecordStatus {
	out := map[RecordPurpose]RecordStatus{}
	for _, r := range records {
		out[r.Purpose] = r.Status
	}
	return out
}

func TestRecordLookupsAskOnlyForTheRecordsTheProviderDoesNotCheck(t *testing.T) {
	mx, txt := RecordLookups(checkedRecords())
	if len(mx) != 1 || mx[0] != "reply.loja.com.br" {
		t.Fatalf("mx lookups = %v", mx)
	}
	if len(txt) != 1 || txt[0] != "_dmarc.loja.com.br" {
		t.Fatalf("txt lookups = %v", txt)
	}
}

func TestCheckRecords(t *testing.T) {
	cases := []struct {
		name    string
		state   IdentityState
		answers DNSAnswers
		want    map[RecordPurpose]RecordStatus
	}{
		{
			name:  "everything confirmed",
			state: IdentityState{DKIMVerified: true, MailFromVerified: true},
			answers: DNSAnswers{
				MX:  map[string]DNSAnswer{"reply.loja.com.br": {Values: []string{"Inbound.Provider.Test."}}},
				TXT: map[string]DNSAnswer{"_dmarc.loja.com.br": {Values: []string{"v=DMARC1; p=quarantine; rua=mailto:x@loja.com.br"}}},
			},
			want: map[RecordPurpose]RecordStatus{RecordDKIM: RecordVerified, RecordMailFromMX: RecordVerified, RecordMailFromSPF: RecordVerified, RecordReplyMX: RecordVerified, RecordDMARC: RecordVerified},
		},
		{
			name:  "nothing published yet",
			state: IdentityState{},
			answers: DNSAnswers{
				MX:  map[string]DNSAnswer{"reply.loja.com.br": {}},
				TXT: map[string]DNSAnswer{"_dmarc.loja.com.br": {Values: []string{"v=spf1 -all"}}},
			},
			want: map[RecordPurpose]RecordStatus{RecordDKIM: RecordPending, RecordMailFromMX: RecordPending, RecordMailFromSPF: RecordPending, RecordReplyMX: RecordPending, RecordDMARC: RecordPending},
		},
		{
			name:  "a reply mx pointing elsewhere is pending",
			state: IdentityState{DKIMVerified: true},
			answers: DNSAnswers{
				MX:  map[string]DNSAnswer{"reply.loja.com.br": {Values: []string{"mx.other.test"}}},
				TXT: map[string]DNSAnswer{"_dmarc.loja.com.br": {Values: []string{"v=DMARC1; p=none;"}}},
			},
			want: map[RecordPurpose]RecordStatus{RecordDKIM: RecordVerified, RecordMailFromMX: RecordPending, RecordMailFromSPF: RecordPending, RecordReplyMX: RecordPending, RecordDMARC: RecordVerified},
		},
		{
			name:    "failed or missing lookups are unknown",
			state:   IdentityState{DKIMVerified: true, MailFromVerified: true},
			answers: DNSAnswers{MX: map[string]DNSAnswer{"reply.loja.com.br": {Failed: true}}},
			want:    map[RecordPurpose]RecordStatus{RecordDKIM: RecordVerified, RecordMailFromMX: RecordVerified, RecordMailFromSPF: RecordVerified, RecordReplyMX: RecordUnknown, RecordDMARC: RecordUnknown},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			records := checkedRecords()
			got := statusesOf(CheckRecords(records, tc.state, tc.answers))
			for purpose, want := range tc.want {
				if got[purpose] != want {
					t.Errorf("%s = %q, want %q", purpose, got[purpose], want)
				}
			}
			for _, r := range records {
				if r.Status != "" {
					t.Fatal("CheckRecords must not change its input")
				}
			}
		})
	}
}

func TestUncheckedRecordsArePending(t *testing.T) {
	if (DNSRecord{}).CurrentStatus() != RecordPending {
		t.Fatal("a record never checked is pending")
	}
	if (DNSRecord{Status: RecordUnknown}).CurrentStatus() != RecordUnknown {
		t.Fatal("a stored status is kept")
	}
	if (DNSRecord{Status: "bogus"}).CurrentStatus() != RecordPending {
		t.Fatal("an unknown stored value reads as pending")
	}
}
