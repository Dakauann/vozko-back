package route53dns

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"

	es "vozko/domain/emailsending"
	"vozko/infra/route53dns/route53fake"
	"vozko/infra/ses"
)

const (
	probationZone = "ZPROBATION"
	trustedZone   = "ZTRUSTED"
)

type countingLimiter struct {
	mu    sync.Mutex
	lanes []es.ProviderLane
}

func (l *countingLimiter) Await(_ context.Context, lane es.ProviderLane) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lanes = append(l.lanes, lane)
	return nil
}

func (l *countingLimiter) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.lanes)
}

type harness struct {
	fake    *route53fake.Server
	dns     *DNSRecords
	limiter *countingLimiter
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	fake := route53fake.New()
	fake.AddZone(probationZone, "envios.test")
	fake.AddZone(trustedZone, "hml.mail.test")
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	limiter := &countingLimiter{}
	client, err := NewClient(aws.Config{
		Region:      "sa-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("AKID", "SECRET", ""),
	}, srv.URL, ses.PaceCalls(limiter, EveryCall))
	if err != nil {
		t.Fatal(err)
	}
	dns, err := New(client, Config{Zones: map[string]string{"envios.test": probationZone, "hml.mail.test": "/hostedzone/" + trustedZone}})
	if err != nil {
		t.Fatal(err)
	}
	return &harness{fake: fake, dns: dns, limiter: limiter}
}

func (h *harness) changeBodies() []string {
	var out []string
	for _, r := range h.fake.Requests() {
		if r.Method == http.MethodPost {
			out = append(out, r.Body)
		}
	}
	return out
}

func sharedRecords() []es.DNSRecord {
	return []es.DNSRecord{
		{Purpose: es.RecordDKIM, Type: "CNAME", Name: "aaa._domainkey.joana.envios.test", Value: "aaa.dkim.amazonses.com"},
		{Purpose: es.RecordDKIM, Type: "CNAME", Name: "bbb._domainkey.joana.envios.test", Value: "bbb.dkim.amazonses.com"},
		{Purpose: es.RecordMailFromMX, Type: "MX", Name: "bounce.joana.envios.test", Value: "feedback-smtp.sa-east-1.amazonses.com", Priority: 10},
		{Purpose: es.RecordMailFromSPF, Type: "TXT", Name: "bounce.joana.envios.test", Value: "v=spf1 include:amazonses.com ~all"},
		{Purpose: es.RecordReplyMX, Type: "MX", Name: "joana.envios.test", Value: "inbound-smtp.sa-east-1.amazonaws.com", Priority: 10},
	}
}

func setsByKey(sets []route53fake.RecordSet) map[string]route53fake.RecordSet {
	out := map[string]route53fake.RecordSet{}
	for _, s := range sets {
		out[s.Type+" "+s.Name] = s
	}
	return out
}

func TestUpsertWritesEveryRecordOfADomainInOneChangeBatch(t *testing.T) {
	h := newHarness(t)
	if err := h.dns.UpsertRecords(context.Background(), "envios.test", sharedRecords()); err != nil {
		t.Fatal(err)
	}
	bodies := h.changeBodies()
	if len(bodies) != 1 {
		t.Fatalf("change calls = %d, want one batch for the domain", len(bodies))
	}
	if strings.Count(bodies[0], "<Action>UPSERT</Action>") != 5 {
		t.Fatalf("batch = %s, want five UPSERT changes", bodies[0])
	}
	got := setsByKey(h.fake.RecordSets(probationZone))
	want := map[string]route53fake.RecordSet{
		"CNAME aaa._domainkey.joana.envios.test.": {Name: "aaa._domainkey.joana.envios.test.", Type: "CNAME", TTL: DefaultTTL, Values: []string{"aaa.dkim.amazonses.com"}},
		"CNAME bbb._domainkey.joana.envios.test.": {Name: "bbb._domainkey.joana.envios.test.", Type: "CNAME", TTL: DefaultTTL, Values: []string{"bbb.dkim.amazonses.com"}},
		"MX bounce.joana.envios.test.":            {Name: "bounce.joana.envios.test.", Type: "MX", TTL: DefaultTTL, Values: []string{"10 feedback-smtp.sa-east-1.amazonses.com"}},
		"TXT bounce.joana.envios.test.":           {Name: "bounce.joana.envios.test.", Type: "TXT", TTL: DefaultTTL, Values: []string{`"v=spf1 include:amazonses.com ~all"`}},
		"MX joana.envios.test.":                   {Name: "joana.envios.test.", Type: "MX", TTL: DefaultTTL, Values: []string{"10 inbound-smtp.sa-east-1.amazonaws.com"}},
	}
	if len(got) != len(want) {
		t.Fatalf("record sets = %+v", got)
	}
	for key, w := range want {
		g := got[key]
		if g.TTL != w.TTL || !slices.Equal(g.Values, w.Values) {
			t.Fatalf("%s = %+v, want %+v", key, g, w)
		}
	}
	if DefaultTTL != 300 {
		t.Fatalf("default ttl = %d, want the zones' 300", DefaultTTL)
	}
}

func TestUpsertAgainReplacesTheValuesWithoutDuplicates(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if err := h.dns.UpsertRecords(ctx, "envios.test", sharedRecords()); err != nil {
		t.Fatal(err)
	}
	changed := sharedRecords()
	changed[0].Value = "zzz.dkim.amazonses.com"
	if err := h.dns.UpsertRecords(ctx, "envios.test", changed); err != nil {
		t.Fatal(err)
	}
	got := setsByKey(h.fake.RecordSets(probationZone))
	if len(got) != 5 || !slices.Equal(got["CNAME aaa._domainkey.joana.envios.test."].Values, []string{"zzz.dkim.amazonses.com"}) {
		t.Fatalf("record sets after a second upsert = %+v", got)
	}
}

func TestTXTValuesSharingANameBecomeOneRecordSet(t *testing.T) {
	h := newHarness(t)
	records := []es.DNSRecord{
		{Type: "TXT", Name: "joana.envios.test", Value: "first"},
		{Type: "TXT", Name: "joana.envios.test", Value: "second"},
		{Type: "TXT", Name: "joana.envios.test", Value: "first"},
	}
	if err := h.dns.UpsertRecords(context.Background(), "envios.test", records); err != nil {
		t.Fatal(err)
	}
	sets := h.fake.RecordSets(probationZone)
	if len(sets) != 1 || !slices.Equal(sets[0].Values, []string{`"first"`, `"second"`}) {
		t.Fatalf("sets = %+v, want one TXT set with both values once", sets)
	}
	if body := h.changeBodies()[0]; strings.Count(body, "<Change>") != 1 {
		t.Fatalf("batch = %s, want one change for the shared name", body)
	}
}

func TestLongTXTValuesAreSplitIntoQuotedStringsOf255(t *testing.T) {
	long := strings.Repeat("a", 300) + `"q"`
	got := txtValue(long)
	want := `"` + strings.Repeat("a", 255) + `" "` + strings.Repeat("a", 45) + `\"q\""`
	if got != want {
		t.Fatalf("txt = %s\nwant %s", got, want)
	}
	if txtValue(`"already quoted"`) != `"already quoted"` {
		t.Fatal("a quoted value must be kept as is")
	}
}

func TestDeleteRemovesExactlyTheCurrentRecordSets(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if err := h.dns.UpsertRecords(ctx, "envios.test", sharedRecords()); err != nil {
		t.Fatal(err)
	}
	h.fake.Put(probationZone, route53fake.RecordSet{Name: "_dmarc.envios.test", Type: "TXT", TTL: 3600, Values: []string{`"v=DMARC1; p=none;"`}})
	if err := h.dns.DeleteRecords(ctx, "envios.test", sharedRecords()); err != nil {
		t.Fatal(err)
	}
	left := h.fake.RecordSets(probationZone)
	if len(left) != 1 || left[0].Name != "_dmarc.envios.test." {
		t.Fatalf("left = %+v, want only the hand made DMARC record", left)
	}
	bodies := h.changeBodies()
	last := bodies[len(bodies)-1]
	if len(bodies) != 2 || strings.Count(last, "<Action>DELETE</Action>") != 5 || !strings.Contains(last, "<TTL>300</TTL>") {
		t.Fatalf("delete batch = %s, want one batch deleting the five sets with their current ttl", last)
	}
}

func TestDeleteKeepsOtherValuesOfASharedTXTName(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.fake.Put(probationZone, route53fake.RecordSet{Name: "joana.envios.test.", Type: "TXT", TTL: 600, Values: []string{`"google-site-verification=abc"`, `"ours"`}})
	if err := h.dns.DeleteRecords(ctx, "envios.test", []es.DNSRecord{{Type: "TXT", Name: "joana.envios.test", Value: "ours"}}); err != nil {
		t.Fatal(err)
	}
	sets := h.fake.RecordSets(probationZone)
	if len(sets) != 1 || !slices.Equal(sets[0].Values, []string{`"google-site-verification=abc"`}) || sets[0].TTL != 600 {
		t.Fatalf("sets = %+v, want the other value kept with its ttl", sets)
	}
}

func TestDeletingRecordsThatAreGoneIsDone(t *testing.T) {
	h := newHarness(t)
	if err := h.dns.DeleteRecords(context.Background(), "envios.test", sharedRecords()); err != nil {
		t.Fatal(err)
	}
	if len(h.changeBodies()) != 0 {
		t.Fatal("nothing to delete must send no change")
	}
}

func TestADeleteRacingAnotherWriterReadsAgainAndFinishes(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if err := h.dns.UpsertRecords(ctx, "envios.test", sharedRecords()); err != nil {
		t.Fatal(err)
	}
	h.fake.RefuseNextChange(http.StatusBadRequest, "InvalidChangeBatch", "Tried to delete resource record set [name='joana.envios.test.', type='MX'] but it was not found")
	if err := h.dns.DeleteRecords(ctx, "envios.test", sharedRecords()); err != nil {
		t.Fatalf("a not found answer must be read again and finished: %v", err)
	}
	if left := h.fake.RecordSets(probationZone); len(left) != 0 {
		t.Fatalf("left = %+v", left)
	}
}

func TestADeleteThatNeverSettlesFails(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if err := h.dns.UpsertRecords(ctx, "envios.test", sharedRecords()); err != nil {
		t.Fatal(err)
	}
	for range changeAttempts {
		h.fake.RefuseNextChange(http.StatusBadRequest, "InvalidChangeBatch", "values do not match")
	}
	if err := h.dns.DeleteRecords(ctx, "envios.test", sharedRecords()); !errors.Is(err, ErrChangeConflict) {
		t.Fatalf("err = %v, want the conflict", err)
	}
}

func TestEveryCallWaitsForTheLimiter(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if err := h.dns.UpsertRecords(ctx, "envios.test", sharedRecords()); err != nil {
		t.Fatal(err)
	}
	if err := h.dns.DeleteRecords(ctx, "envios.test", sharedRecords()); err != nil {
		t.Fatal(err)
	}
	if got, sent := h.limiter.count(), len(h.fake.Requests()); got != sent || got == 0 {
		t.Fatalf("limiter waits = %d, requests = %d", got, sent)
	}
}

func TestTheZoneIsCheckedOnceAgainstTheSharedDomain(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	for range 2 {
		if err := h.dns.UpsertRecords(ctx, "envios.test", sharedRecords()); err != nil {
			t.Fatal(err)
		}
	}
	gets := 0
	for _, r := range h.fake.Requests() {
		if r.Method == http.MethodGet && strings.HasSuffix(r.Path, "/hostedzone/"+probationZone) {
			gets++
		}
	}
	if gets != 1 {
		t.Fatalf("zone reads = %d, want one per process", gets)
	}
	trusted := []es.DNSRecord{{Type: "MX", Name: "joana.hml.mail.test", Value: "inbound-smtp.sa-east-1.amazonaws.com", Priority: 10}}
	if err := h.dns.UpsertRecords(ctx, "hml.mail.test", trusted); err != nil {
		t.Fatalf("a zone id with its /hostedzone/ prefix: %v", err)
	}
}

func TestAZoneThatServesAnotherDomainIsRefused(t *testing.T) {
	h := newHarness(t)
	h.fake.AddZone("ZOTHER", "other.test")
	dns, err := New(h.dns.api, Config{Zones: map[string]string{"envios.test": "ZOTHER"}})
	if err != nil {
		t.Fatal(err)
	}
	err = dns.UpsertRecords(context.Background(), "envios.test", sharedRecords())
	if !errors.Is(err, ErrZoneMismatch) {
		t.Fatalf("err = %v, want the zone mismatch", err)
	}
	if len(h.changeBodies()) != 0 {
		t.Fatal("no change may reach a zone of another domain")
	}
}

func TestAParentZoneServesItsDelegatedSharedDomain(t *testing.T) {
	h := newHarness(t)
	h.fake.AddZone("ZAPEX", "mail.test")
	dns, err := New(h.dns.api, Config{Zones: map[string]string{"hml.mail.test": "ZAPEX"}})
	if err != nil {
		t.Fatal(err)
	}
	records := []es.DNSRecord{{Type: "MX", Name: "joana.hml.mail.test", Value: "inbound-smtp.sa-east-1.amazonaws.com", Priority: 10}}
	if err := dns.UpsertRecords(context.Background(), "hml.mail.test", records); err != nil {
		t.Fatal(err)
	}
}

func TestRecordsOutsideTheSharedDomainOrOfOtherTypesAreRefused(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	outside := []es.DNSRecord{{Type: "MX", Name: "joana.mail.test", Value: "x", Priority: 10}}
	if err := h.dns.UpsertRecords(ctx, "envios.test", outside); !errors.Is(err, ErrRecordOutsideZone) {
		t.Fatalf("err = %v", err)
	}
	apex := []es.DNSRecord{{Type: "MX", Name: "envios.test", Value: "x", Priority: 10}}
	if err := h.dns.DeleteRecords(ctx, "envios.test", apex); !errors.Is(err, ErrRecordOutsideZone) {
		t.Fatalf("err = %v, the shared apex is not ours", err)
	}
	other := []es.DNSRecord{{Type: "A", Name: "joana.envios.test", Value: "1.2.3.4"}}
	if err := h.dns.UpsertRecords(ctx, "envios.test", other); !errors.Is(err, ErrUnsupportedRecord) {
		t.Fatalf("err = %v", err)
	}
	if err := h.dns.UpsertRecords(ctx, "unknown.test", sharedRecords()); !errors.Is(err, ErrUnknownZone) {
		t.Fatalf("err = %v", err)
	}
	if len(h.fake.Requests()) != 0 {
		t.Fatal("a refused request must not reach Route 53")
	}
}

func TestOnlyRecordSetsAreEverChanged(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if err := h.dns.UpsertRecords(ctx, "envios.test", sharedRecords()); err != nil {
		t.Fatal(err)
	}
	if err := h.dns.DeleteRecords(ctx, "envios.test", sharedRecords()); err != nil {
		t.Fatal(err)
	}
	for _, r := range h.fake.Requests() {
		allowed := (r.Method == http.MethodGet && strings.HasSuffix(r.Path, "/hostedzone/"+probationZone)) ||
			(r.Method == http.MethodGet && strings.HasSuffix(r.Path, "/rrset")) ||
			(r.Method == http.MethodPost && strings.HasSuffix(r.Path, "/rrset"))
		if !allowed {
			t.Fatalf("request %s %s is not a record set call", r.Method, r.Path)
		}
	}
}

func TestAnAccessErrorIsReturned(t *testing.T) {
	h := newHarness(t)
	h.fake.RefuseNext(http.StatusForbidden, "AccessDenied", "not authorized to perform route53:GetHostedZone")
	err := h.dns.UpsertRecords(context.Background(), "envios.test", sharedRecords())
	if err == nil || !strings.Contains(err.Error(), "AccessDenied") {
		t.Fatalf("err = %v", err)
	}
	if err := h.dns.UpsertRecords(context.Background(), "envios.test", sharedRecords()); err != nil {
		t.Fatalf("a failed zone check is retried on the next call: %v", err)
	}
}

func TestNewRefusesAnIncompleteConfiguration(t *testing.T) {
	h := newHarness(t)
	cases := []Config{{}, {Zones: map[string]string{"envios.test": ""}}, {Zones: map[string]string{"": "Z1"}}}
	for _, cfg := range cases {
		if _, err := New(h.dns.api, cfg); !errors.Is(err, ErrConfig) {
			t.Fatalf("%+v: err = %v", cfg, err)
		}
	}
	if _, err := New(nil, Config{Zones: map[string]string{"envios.test": "Z1"}}); !errors.Is(err, ErrConfig) {
		t.Fatalf("err = %v", err)
	}
	if _, err := NewClient(aws.Config{Region: "sa-east-1"}, "", nil); !errors.Is(err, ErrConfig) {
		t.Fatalf("a client without pacing: err = %v", err)
	}
}
