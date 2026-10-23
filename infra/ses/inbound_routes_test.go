package ses

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	sesv1 "github.com/aws/aws-sdk-go-v2/service/ses"
	sesv1types "github.com/aws/aws-sdk-go-v2/service/ses/types"

	"vozko/domain/cache"
	"vozko/domain/emailsending"
)

type fakeReceiptAPI struct {
	mu        sync.Mutex
	set       string
	rules     []sesv1types.ReceiptRule
	filters   []sesv1types.ReceiptFilter
	calls     []string
	createErr error
	failOnce  map[string]error
}

func newFakeReceiptAPI(rules ...sesv1types.ReceiptRule) *fakeReceiptAPI {
	return &fakeReceiptAPI{set: "main", rules: rules, failOnce: map[string]error{}}
}

func (f *fakeReceiptAPI) record(call string) error {
	f.calls = append(f.calls, call)
	if err, ok := f.failOnce[call]; ok {
		delete(f.failOnce, call)
		return err
	}
	return nil
}

func (f *fakeReceiptAPI) DescribeActiveReceiptRuleSet(context.Context, *sesv1.DescribeActiveReceiptRuleSetInput, ...func(*sesv1.Options)) (*sesv1.DescribeActiveReceiptRuleSetOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("describe"); err != nil {
		return nil, err
	}
	if f.set == "" {
		return &sesv1.DescribeActiveReceiptRuleSetOutput{}, nil
	}
	rules := make([]sesv1types.ReceiptRule, len(f.rules))
	for i, r := range f.rules {
		r.Recipients = slices.Clone(r.Recipients)
		rules[i] = r
	}
	return &sesv1.DescribeActiveReceiptRuleSetOutput{Metadata: &sesv1types.ReceiptRuleSetMetadata{Name: aws.String(f.set)}, Rules: rules}, nil
}

func (f *fakeReceiptAPI) index(name string) int {
	for i, r := range f.rules {
		if aws.ToString(r.Name) == name {
			return i
		}
	}
	return -1
}

func (f *fakeReceiptAPI) CreateReceiptRule(_ context.Context, in *sesv1.CreateReceiptRuleInput, _ ...func(*sesv1.Options)) (*sesv1.CreateReceiptRuleOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("create " + aws.ToString(in.Rule.Name)); err != nil {
		return nil, err
	}
	if f.createErr != nil {
		return nil, f.createErr
	}
	if f.index(aws.ToString(in.Rule.Name)) >= 0 {
		return nil, &sesv1types.AlreadyExistsException{Message: aws.String("exists")}
	}
	f.rules = append(f.rules, *in.Rule)
	return &sesv1.CreateReceiptRuleOutput{}, nil
}

func (f *fakeReceiptAPI) UpdateReceiptRule(_ context.Context, in *sesv1.UpdateReceiptRuleInput, _ ...func(*sesv1.Options)) (*sesv1.UpdateReceiptRuleOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("update " + aws.ToString(in.Rule.Name)); err != nil {
		return nil, err
	}
	i := f.index(aws.ToString(in.Rule.Name))
	if i < 0 {
		return nil, &sesv1types.RuleDoesNotExistException{Message: aws.String("gone")}
	}
	f.rules[i] = *in.Rule
	return &sesv1.UpdateReceiptRuleOutput{}, nil
}

func (f *fakeReceiptAPI) DeleteReceiptRule(_ context.Context, in *sesv1.DeleteReceiptRuleInput, _ ...func(*sesv1.Options)) (*sesv1.DeleteReceiptRuleOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("delete " + aws.ToString(in.RuleName)); err != nil {
		return nil, err
	}
	i := f.index(aws.ToString(in.RuleName))
	if i < 0 {
		return nil, &sesv1types.RuleDoesNotExistException{Message: aws.String("gone")}
	}
	f.rules = append(f.rules[:i], f.rules[i+1:]...)
	return &sesv1.DeleteReceiptRuleOutput{}, nil
}

func (f *fakeReceiptAPI) ListReceiptFilters(context.Context, *sesv1.ListReceiptFiltersInput, ...func(*sesv1.Options)) (*sesv1.ListReceiptFiltersOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("list filters"); err != nil {
		return nil, err
	}
	return &sesv1.ListReceiptFiltersOutput{Filters: slices.Clone(f.filters)}, nil
}

func (f *fakeReceiptAPI) CreateReceiptFilter(_ context.Context, in *sesv1.CreateReceiptFilterInput, _ ...func(*sesv1.Options)) (*sesv1.CreateReceiptFilterOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("create filter " + aws.ToString(in.Filter.Name)); err != nil {
		return nil, err
	}
	if len(f.filters) >= emailsending.InboundIPFiltersPerAccount {
		return nil, &sesv1types.LimitExceededException{Message: aws.String("too many filters")}
	}
	f.filters = append(f.filters, *in.Filter)
	return &sesv1.CreateReceiptFilterOutput{}, nil
}

func (f *fakeReceiptAPI) DeleteReceiptFilter(_ context.Context, in *sesv1.DeleteReceiptFilterInput, _ ...func(*sesv1.Options)) (*sesv1.DeleteReceiptFilterOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.record("delete filter " + aws.ToString(in.FilterName)); err != nil {
		return nil, err
	}
	for i, flt := range f.filters {
		if aws.ToString(flt.Name) == aws.ToString(in.FilterName) {
			f.filters = append(f.filters[:i], f.filters[i+1:]...)
			break
		}
	}
	return &sesv1.DeleteReceiptFilterOutput{}, nil
}

func (f *fakeReceiptAPI) managed() []sesv1types.ReceiptRule {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []sesv1types.ReceiptRule
	for _, r := range f.rules {
		if strings.HasPrefix(aws.ToString(r.Name), managedRulePrefix) {
			out = append(out, r)
		}
	}
	return out
}

func (f *fakeReceiptAPI) recipients() []string {
	var out []string
	for _, r := range f.managed() {
		out = append(out, r.Recipients...)
	}
	sort.Strings(out)
	return out
}

func (f *fakeReceiptAPI) wrote() bool {
	for _, call := range f.calls {
		if strings.HasPrefix(call, "create") || strings.HasPrefix(call, "update") || strings.HasPrefix(call, "delete") {
			return true
		}
	}
	return false
}

type fakeRouteLock struct {
	mu       sync.Mutex
	held     bool
	acquired int
	err      error
}

func (l *fakeRouteLock) Acquire(context.Context, string) (func() (bool, error), error) {
	if l.err != nil {
		return nil, l.err
	}
	l.mu.Lock()
	l.held = true
	l.acquired++
	return func() (bool, error) {
		l.held = false
		l.mu.Unlock()
		return true, nil
	}, nil
}

func testRoutes(t *testing.T, api *fakeReceiptAPI) (*InboundRoutes, *fakeRouteLock) {
	t.Helper()
	lock := &fakeRouteLock{}
	routes, err := NewInboundRoutes(api, lock, InboundRoutesConfig{
		InboundTopicARN: "arn:aws:sns:sa-east-1:123456789012:inbound",
		InboundBucket:   "inbound-bucket",
	})
	if err != nil {
		t.Fatal(err)
	}
	return routes, lock
}

var routesCtx = context.Background()

func TestNewInboundRoutesRefusesIncompleteConfig(t *testing.T) {
	full := InboundRoutesConfig{InboundTopicARN: "arn", InboundBucket: "b"}
	cases := []struct {
		name string
		api  ReceiptAPI
		lock cache.Locker
		cfg  InboundRoutesConfig
	}{
		{"no api", nil, &fakeRouteLock{}, full},
		{"no lock", newFakeReceiptAPI(), nil, full},
		{"no topic", newFakeReceiptAPI(), &fakeRouteLock{}, InboundRoutesConfig{InboundBucket: "b"}},
		{"no bucket", newFakeReceiptAPI(), &fakeRouteLock{}, InboundRoutesConfig{InboundTopicARN: "arn"}},
	}
	for _, tc := range cases {
		if _, err := NewInboundRoutes(tc.api, tc.lock, tc.cfg); !errors.Is(err, ErrIncompleteConfig) {
			t.Errorf("%s: err = %v", tc.name, err)
		}
	}
}

func TestEnsureReplyRouteListsOnlyTheTwoAddresses(t *testing.T) {
	api := newFakeReceiptAPI()
	routes, lock := testRoutes(t, api)
	if err := routes.EnsureReplyRoute(routesCtx, "Reply.Loja.com.br."); err != nil {
		t.Fatal(err)
	}
	want := []string{"reply@reply.loja.com.br", "unsubscribe@reply.loja.com.br"}
	if got := api.recipients(); !slices.Equal(got, want) {
		t.Fatalf("recipients = %v, want %v", got, want)
	}
	rule := api.managed()[0]
	s3 := rule.Actions[0].S3Action
	if !rule.Enabled || !rule.ScanEnabled || len(rule.Actions) != 1 || aws.ToString(s3.BucketName) != "inbound-bucket" ||
		aws.ToString(s3.ObjectKeyPrefix) != emailsending.InboundObjectPrefix || aws.ToString(s3.TopicArn) != "arn:aws:sns:sa-east-1:123456789012:inbound" {
		t.Fatalf("rule = %+v action = %+v", rule, s3)
	}
	if lock.acquired != 1 || lock.held {
		t.Fatalf("lock acquired %d, still held %v", lock.acquired, lock.held)
	}
	calls := len(api.calls)
	if err := routes.EnsureReplyRoute(routesCtx, "reply.loja.com.br"); err != nil {
		t.Fatal(err)
	}
	if len(api.calls) != calls+1 || !slices.Equal(api.recipients(), want) {
		t.Fatalf("a second ensure wrote again: %v", api.calls[calls:])
	}
	if err := routes.EnsureReplyRoute(routesCtx, "not a domain"); err == nil {
		t.Fatal("an invalid domain is refused")
	}
}

func TestReplyRoutesPackConditionsAtTheRuleLimit(t *testing.T) {
	api := newFakeReceiptAPI()
	routes, _ := testRoutes(t, api)
	domains := emailsending.InboundReplyDomainsPerRule() + 1
	for i := 0; i < domains; i++ {
		if err := routes.EnsureReplyRoute(routesCtx, fmt.Sprintf("d%03d.envios.test", i)); err != nil {
			t.Fatal(err)
		}
	}
	rules := api.managed()
	if len(rules) != 2 || len(rules[0].Recipients) != emailsending.InboundConditionsPerRule || len(rules[1].Recipients) != 2 {
		t.Fatalf("rules = %d, sizes %d and %d", len(rules), len(rules[0].Recipients), len(rules[len(rules)-1].Recipients))
	}
	if aws.ToString(rules[0].Name) == aws.ToString(rules[1].Name) {
		t.Fatal("rule names must be unique")
	}
}

func TestPauseKeepsUnsubscribeAndResumeRestoresReplies(t *testing.T) {
	api := newFakeReceiptAPI()
	routes, _ := testRoutes(t, api)
	for _, d := range []string{"a.envios.test", "b.envios.test"} {
		if err := routes.EnsureReplyRoute(routesCtx, d); err != nil {
			t.Fatal(err)
		}
	}
	if err := routes.PauseReplies(routesCtx, "a.envios.test"); err != nil {
		t.Fatal(err)
	}
	paused := []string{"reply@b.envios.test", "unsubscribe@a.envios.test", "unsubscribe@b.envios.test"}
	if got := api.recipients(); !slices.Equal(got, paused) {
		t.Fatalf("paused recipients = %v, want %v", got, paused)
	}
	if err := routes.PauseReplies(routesCtx, "a.envios.test"); err != nil || !slices.Equal(api.recipients(), paused) {
		t.Fatalf("a second pause changed the rules: %v", err)
	}
	if err := routes.ResumeReplies(routesCtx, "a.envios.test"); err != nil {
		t.Fatal(err)
	}
	all := []string{"reply@a.envios.test", "reply@b.envios.test", "unsubscribe@a.envios.test", "unsubscribe@b.envios.test"}
	if got := api.recipients(); !slices.Equal(got, all) {
		t.Fatalf("resumed recipients = %v, want %v", got, all)
	}
	if err := routes.ResumeReplies(routesCtx, "never.envios.test"); err != nil {
		t.Fatal(err)
	}
	if got := api.recipients(); !slices.Equal(got, all) {
		t.Fatalf("resuming a domain without a route created one: %v", got)
	}
}

func TestRemoveReplyRouteDeletesARuleItEmpties(t *testing.T) {
	api := newFakeReceiptAPI()
	routes, _ := testRoutes(t, api)
	if err := routes.EnsureReplyRoute(routesCtx, "reply.loja.com.br"); err != nil {
		t.Fatal(err)
	}
	if err := routes.PauseReplies(routesCtx, "reply.loja.com.br"); err != nil {
		t.Fatal(err)
	}
	if err := routes.RemoveReplyRoute(routesCtx, "reply.loja.com.br"); err != nil {
		t.Fatal(err)
	}
	if rules := api.managed(); len(rules) != 0 {
		t.Fatalf("a rule without recipients would accept every address, it must be deleted: %+v", rules)
	}
	if err := routes.RemoveReplyRoute(routesCtx, "reply.loja.com.br"); err != nil {
		t.Fatal(err)
	}
}

func TestPausingTheLastReplyOfARuleNeverLeavesItEmpty(t *testing.T) {
	api := newFakeReceiptAPI(sesv1types.ReceiptRule{Name: aws.String(managedRulePrefix + "001"), Recipients: []string{"reply@x.envios.test"}})
	routes, _ := testRoutes(t, api)
	if err := routes.PauseReplies(routesCtx, "x.envios.test"); err != nil {
		t.Fatal(err)
	}
	if len(api.managed()) != 0 {
		t.Fatalf("rules = %+v", api.managed())
	}
}

func fullRuleSet(prefix string, recipientsPerRule int) []sesv1types.ReceiptRule {
	var rules []sesv1types.ReceiptRule
	for i := 0; i < emailsending.InboundRulesPerRuleSet; i++ {
		recipients := make([]string, recipientsPerRule)
		for j := range recipients {
			recipients[j] = fmt.Sprintf("reply@d%d-%d.envios.test", i, j)
		}
		rules = append(rules, sesv1types.ReceiptRule{Name: aws.String(fmt.Sprintf("%s%03d", prefix, i)), Recipients: recipients})
	}
	return rules
}

func TestReplyRouteCapacityIsACodedError(t *testing.T) {
	cases := []struct {
		name  string
		rules []sesv1types.ReceiptRule
	}{
		{name: "every rule of ours is full", rules: fullRuleSet(managedRulePrefix, emailsending.InboundConditionsPerRule)},
		{name: "rules that are not ours count toward the rule limit", rules: fullRuleSet("other-", 1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api := newFakeReceiptAPI(tc.rules...)
			routes, _ := testRoutes(t, api)
			if err := routes.EnsureReplyRoute(routesCtx, "new.envios.test"); !errors.Is(err, emailsending.ErrInboundCapacityReached) {
				t.Fatalf("err = %v", err)
			}
			if api.wrote() {
				t.Fatalf("a full rule set was written: %v", api.calls)
			}
		})
	}
}

func TestReplyRoutesRetryARaceOnAFreshRead(t *testing.T) {
	api := newFakeReceiptAPI(sesv1types.ReceiptRule{Name: aws.String(managedRulePrefix + "001"), Recipients: []string{"reply@x.envios.test"}})
	api.failOnce["update "+managedRulePrefix+"001"] = &sesv1types.RuleDoesNotExistException{Message: aws.String("deleted elsewhere")}
	routes, _ := testRoutes(t, api)
	if err := routes.EnsureReplyRoute(routesCtx, "a.envios.test"); err != nil {
		t.Fatal(err)
	}
	if got := api.recipients(); !slices.Contains(got, "reply@a.envios.test") || !slices.Contains(got, "unsubscribe@a.envios.test") {
		t.Fatalf("recipients after retry = %v", got)
	}
	describes := 0
	for _, c := range api.calls {
		if c == "describe" {
			describes++
		}
	}
	if describes != 2 {
		t.Fatalf("describes = %d, want a fresh read before the retry", describes)
	}
}

func TestReplyRoutesFailClosed(t *testing.T) {
	t.Run("no active rule set", func(t *testing.T) {
		api := newFakeReceiptAPI()
		api.set = ""
		routes, _ := testRoutes(t, api)
		if err := routes.EnsureReplyRoute(routesCtx, "a.envios.test"); !errors.Is(err, ErrNoActiveRuleSet) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("the lock cannot be taken", func(t *testing.T) {
		api := newFakeReceiptAPI()
		routes, lock := testRoutes(t, api)
		lock.err = errors.New("redis down")
		if err := routes.PauseReplies(routesCtx, "a.envios.test"); err == nil || len(api.calls) != 0 {
			t.Fatalf("err = %v calls = %v", err, api.calls)
		}
	})
	t.Run("a write failure is reported", func(t *testing.T) {
		api := newFakeReceiptAPI()
		api.createErr = errors.New("throttled")
		routes, _ := testRoutes(t, api)
		if err := routes.EnsureReplyRoute(routesCtx, "a.envios.test"); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestAdoptLegacyRoutesReplacesDomainWideRules(t *testing.T) {
	api := newFakeReceiptAPI(
		sesv1types.ReceiptRule{Name: aws.String("in-envios.test"), Recipients: []string{".envios.test"}},
		sesv1types.ReceiptRule{Name: aws.String("in-reply.loja.com.br"), Recipients: []string{"reply.loja.com.br"}},
		sesv1types.ReceiptRule{Name: aws.String("in-0123456789abcdef0123456789abcdef"), Recipients: []string{"reply.longdomain.com"}},
		sesv1types.ReceiptRule{Name: aws.String("someone-else"), Recipients: []string{"ops@corp.com"}},
	)
	routes, _ := testRoutes(t, api)
	err := routes.AdoptLegacyRoutes(routesCtx, []emailsending.ReplyRoute{
		{ReplyDomain: "joana.envios.test"},
		{ReplyDomain: "reply.loja.com.br", Paused: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"reply@joana.envios.test", "unsubscribe@joana.envios.test", "unsubscribe@reply.loja.com.br"}
	if got := api.recipients(); !slices.Equal(got, want) {
		t.Fatalf("recipients = %v, want %v", got, want)
	}
	for _, legacy := range []string{"in-envios.test", "in-reply.loja.com.br", "in-0123456789abcdef0123456789abcdef"} {
		if api.index(legacy) >= 0 {
			t.Fatalf("legacy rule %s survived", legacy)
		}
	}
	if api.index("someone-else") < 0 {
		t.Fatal("a rule that is not ours was deleted")
	}
	calls := len(api.calls)
	if err := routes.AdoptLegacyRoutes(routesCtx, []emailsending.ReplyRoute{{ReplyDomain: "other.envios.test"}}); err != nil {
		t.Fatal(err)
	}
	if len(api.calls) != calls+1 {
		t.Fatalf("adoption without legacy rules must only read: %v", api.calls[calls:])
	}
}

func blockFilter(name, cidr string) sesv1types.ReceiptFilter {
	return sesv1types.ReceiptFilter{Name: aws.String(name), IpFilter: &sesv1types.ReceiptIpFilter{Cidr: aws.String(cidr), Policy: sesv1types.ReceiptFilterPolicyBlock}}
}

func isAlnum(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

func TestBlockSender(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	until := now.Add(emailsending.InboundBlockDuration)
	api := newFakeReceiptAPI()
	routes, _ := testRoutes(t, api)
	ip := netip.MustParseAddr("203.0.113.7")
	created, err := routes.BlockSender(routesCtx, ip, until)
	if err != nil || !created {
		t.Fatalf("block = %v, %v", created, err)
	}
	f := api.filters[0]
	if f.IpFilter.Policy != sesv1types.ReceiptFilterPolicyBlock || aws.ToString(f.IpFilter.Cidr) != "203.0.113.7/32" {
		t.Fatalf("filter = %+v", f.IpFilter)
	}
	if name := aws.ToString(f.Name); len(name) > 64 || !strings.HasPrefix(name, blockFilterPrefix) {
		t.Fatalf("filter name %q", name)
	}
	again, err := routes.BlockSender(routesCtx, ip, until.Add(time.Hour))
	if err != nil || again || len(api.filters) != 1 {
		t.Fatalf("a second block of the same sender = %v, %v, filters %d", again, err, len(api.filters))
	}
	if created, err := routes.BlockSender(routesCtx, netip.MustParseAddr("::ffff:203.0.113.7"), until); err != nil || created {
		t.Fatalf("an ipv4 mapped address is the same sender: %v, %v", created, err)
	}
	if created, err := routes.BlockSender(routesCtx, netip.MustParseAddr("2001:db8::"), until); err != nil || !created {
		t.Fatalf("ipv6 block = %v, %v", created, err)
	}
	if got := aws.ToString(api.filters[1].IpFilter.Cidr); got != "2001:db8::/128" {
		t.Fatalf("ipv6 cidr = %q", got)
	}
	n := aws.ToString(api.filters[1].Name)
	if len(n) > 64 || strings.ContainsAny(n, ":.") || !isAlnum(n[len(n)-1]) {
		t.Fatalf("ipv6 filter name %q breaks the SES naming rules", n)
	}
	if _, err := routes.BlockSender(routesCtx, netip.Addr{}, until); err == nil {
		t.Fatal("an invalid address is refused")
	}
}

func TestBlockSenderRespectsABlockThatIsNotOurs(t *testing.T) {
	api := newFakeReceiptAPI()
	api.filters = []sesv1types.ReceiptFilter{blockFilter("manual", "203.0.113.0/24")}
	routes, _ := testRoutes(t, api)
	if created, err := routes.BlockSender(routesCtx, netip.MustParseAddr("203.0.113.9"), time.Now()); err != nil || created {
		t.Fatalf("block = %v, %v", created, err)
	}
}

func TestBlockSenderEvictsTheSoonestExpiringBlockAtTheQuota(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	api := newFakeReceiptAPI()
	api.filters = append(api.filters, blockFilter("manual", "198.51.100.1"))
	for i := 1; i < emailsending.InboundIPFiltersPerAccount; i++ {
		ip := netip.AddrFrom4([4]byte{192, 0, 2, byte(i)})
		api.filters = append(api.filters, blockFilter(blockFilterName(ip, now.Add(time.Duration(i)*time.Hour)), ip.String()+"/32"))
	}
	routes, _ := testRoutes(t, api)
	created, err := routes.BlockSender(routesCtx, netip.MustParseAddr("203.0.113.7"), now.Add(emailsending.InboundBlockDuration))
	if err != nil || !created {
		t.Fatalf("block = %v, %v", created, err)
	}
	soonest := blockFilterName(netip.AddrFrom4([4]byte{192, 0, 2, 1}), now.Add(time.Hour))
	if !slices.Contains(api.calls, "delete filter "+soonest) || len(api.filters) != emailsending.InboundIPFiltersPerAccount {
		t.Fatalf("calls = %v filters = %d", api.calls, len(api.filters))
	}
	if !slices.ContainsFunc(api.filters, func(f sesv1types.ReceiptFilter) bool { return aws.ToString(f.Name) == "manual" }) {
		t.Fatal("a filter that is not ours was evicted")
	}
}

func TestBlockSenderWithAQuotaFullOfOtherFiltersFails(t *testing.T) {
	api := newFakeReceiptAPI()
	for i := 0; i < emailsending.InboundIPFiltersPerAccount; i++ {
		api.filters = append(api.filters, blockFilter(fmt.Sprintf("manual-%d", i), fmt.Sprintf("198.51.100.%d", i)))
	}
	routes, _ := testRoutes(t, api)
	if _, err := routes.BlockSender(routesCtx, netip.MustParseAddr("203.0.113.7"), time.Now()); !errors.Is(err, ErrFilterQuotaFull) {
		t.Fatalf("err = %v", err)
	}
}

func TestUnblockExpired(t *testing.T) {
	now := time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
	expired := blockFilterName(netip.MustParseAddr("192.0.2.1"), now.Add(-time.Minute))
	api := newFakeReceiptAPI()
	api.filters = []sesv1types.ReceiptFilter{
		blockFilter(expired, "192.0.2.1/32"),
		blockFilter(blockFilterName(netip.MustParseAddr("192.0.2.2"), now.Add(time.Hour)), "192.0.2.2/32"),
		blockFilter("manual", "198.51.100.1"),
		blockFilter(blockFilterPrefix+"garbage", "198.51.100.2"),
	}
	routes, _ := testRoutes(t, api)
	if err := routes.UnblockExpired(routesCtx, now); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, f := range api.filters {
		names = append(names, aws.ToString(f.Name))
	}
	if len(names) != 3 || slices.Contains(names, expired) {
		t.Fatalf("filters left = %v", names)
	}
}

func TestIPFilterCountCountsEveryFilterOfTheAccount(t *testing.T) {
	api := newFakeReceiptAPI()
	api.filters = []sesv1types.ReceiptFilter{blockFilter("manual", "203.0.113.0/24"), blockFilter(blockFilterPrefix+"1-ab", "198.51.100.1/32")}
	routes, _ := testRoutes(t, api)
	got, err := routes.IPFilterCount(routesCtx)
	if err != nil || got != 2 {
		t.Fatalf("count = %d, %v, want 2", got, err)
	}
	api.failOnce["list filters"] = errors.New("throttled")
	if _, err := routes.IPFilterCount(routesCtx); err == nil {
		t.Fatal("an unreadable filter list is an error, never zero")
	}
}
