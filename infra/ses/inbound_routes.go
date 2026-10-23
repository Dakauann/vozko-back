package ses

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	sesv1 "github.com/aws/aws-sdk-go-v2/service/ses"
	sesv1types "github.com/aws/aws-sdk-go-v2/service/ses/types"

	"vozko/domain/cache"
	"vozko/domain/emailaddress"
	"vozko/domain/emailsending"
)

const (
	managedRulePrefix = "in-replies-"
	legacyRulePrefix  = "in-"
	blockFilterPrefix = "inbound-block-"
	routesLockKey     = "ses:receipt-rules"
	maxRouteAttempts  = 3
)

var (
	ErrNoActiveRuleSet = errors.New("ses: no active receipt rule set")
	ErrFilterQuotaFull = errors.New("ses: every ip address filter is taken by filters this platform does not manage")
)

type ReceiptAPI interface {
	DescribeActiveReceiptRuleSet(ctx context.Context, in *sesv1.DescribeActiveReceiptRuleSetInput, opts ...func(*sesv1.Options)) (*sesv1.DescribeActiveReceiptRuleSetOutput, error)
	CreateReceiptRule(ctx context.Context, in *sesv1.CreateReceiptRuleInput, opts ...func(*sesv1.Options)) (*sesv1.CreateReceiptRuleOutput, error)
	UpdateReceiptRule(ctx context.Context, in *sesv1.UpdateReceiptRuleInput, opts ...func(*sesv1.Options)) (*sesv1.UpdateReceiptRuleOutput, error)
	DeleteReceiptRule(ctx context.Context, in *sesv1.DeleteReceiptRuleInput, opts ...func(*sesv1.Options)) (*sesv1.DeleteReceiptRuleOutput, error)
	ListReceiptFilters(ctx context.Context, in *sesv1.ListReceiptFiltersInput, opts ...func(*sesv1.Options)) (*sesv1.ListReceiptFiltersOutput, error)
	CreateReceiptFilter(ctx context.Context, in *sesv1.CreateReceiptFilterInput, opts ...func(*sesv1.Options)) (*sesv1.CreateReceiptFilterOutput, error)
	DeleteReceiptFilter(ctx context.Context, in *sesv1.DeleteReceiptFilterInput, opts ...func(*sesv1.Options)) (*sesv1.DeleteReceiptFilterOutput, error)
}

type InboundRoutesConfig struct {
	InboundTopicARN string
	InboundBucket   string
}

type InboundRoutes struct {
	api  ReceiptAPI
	lock cache.Locker
	cfg  InboundRoutesConfig
}

var _ emailsending.InboundRoutes = (*InboundRoutes)(nil)

func NewInboundRoutes(api ReceiptAPI, lock cache.Locker, cfg InboundRoutesConfig) (*InboundRoutes, error) {
	if api == nil || lock == nil {
		return nil, fmt.Errorf("%w: receipt clients", ErrIncompleteConfig)
	}
	if strings.TrimSpace(cfg.InboundTopicARN) == "" || strings.TrimSpace(cfg.InboundBucket) == "" {
		return nil, fmt.Errorf("%w: inbound topic and bucket", ErrIncompleteConfig)
	}
	return &InboundRoutes{api: api, lock: lock, cfg: cfg}, nil
}

func (r *InboundRoutes) EnsureReplyRoute(ctx context.Context, replyDomain string) error {
	return r.editDomain(ctx, replyDomain, func(b *ruleBook, domain string) error {
		return b.add(emailsending.InboundRecipients(domain)...)
	})
}

func (r *InboundRoutes) PauseReplies(ctx context.Context, replyDomain string) error {
	return r.editDomain(ctx, replyDomain, func(b *ruleBook, domain string) error {
		b.remove(emailsending.ReplyRecipient(domain))
		return nil
	})
}

func (r *InboundRoutes) ResumeReplies(ctx context.Context, replyDomain string) error {
	return r.editDomain(ctx, replyDomain, func(b *ruleBook, domain string) error {
		if !b.has(emailsending.UnsubscribeRecipient(domain)) {
			return nil
		}
		return b.add(emailsending.ReplyRecipient(domain))
	})
}

func (r *InboundRoutes) RemoveReplyRoute(ctx context.Context, replyDomain string) error {
	return r.editDomain(ctx, replyDomain, func(b *ruleBook, domain string) error {
		b.remove(emailsending.InboundRecipients(domain)...)
		return nil
	})
}

func (r *InboundRoutes) AdoptLegacyRoutes(ctx context.Context, routes []emailsending.ReplyRoute) error {
	return r.edit(ctx, func(b *ruleBook) error {
		if len(b.legacy) == 0 {
			return nil
		}
		for _, route := range routes {
			domain, err := emailaddress.NormalizeDomain(route.ReplyDomain)
			if err != nil {
				log.Printf("[ses] legacy reply route %q skipped: %v", route.ReplyDomain, err)
				continue
			}
			recipients := emailsending.InboundRecipients(domain)
			if route.Paused {
				recipients = []string{emailsending.UnsubscribeRecipient(domain)}
			}
			if err := b.add(recipients...); err != nil {
				return err
			}
		}
		b.dropLegacy = true
		return nil
	})
}

func (r *InboundRoutes) editDomain(ctx context.Context, replyDomain string, change func(*ruleBook, string) error) error {
	domain, err := emailaddress.NormalizeDomain(replyDomain)
	if err != nil {
		return fmt.Errorf("ses: reply route: %w", err)
	}
	return r.edit(ctx, func(b *ruleBook) error { return change(b, domain) })
}

func (r *InboundRoutes) locked(ctx context.Context, work func() error) error {
	release, err := r.lock.Acquire(ctx, routesLockKey)
	if err != nil {
		return fmt.Errorf("ses: receipt rules lock: %w", err)
	}
	defer func() {
		if released, err := release(); err != nil || !released {
			log.Printf("[ses] receipt rules lock released late (released %v): %v", released, err)
		}
	}()
	return work()
}

func (r *InboundRoutes) edit(ctx context.Context, change func(*ruleBook) error) error {
	return r.locked(ctx, func() error {
		var err error
		for attempt := 0; attempt < maxRouteAttempts; attempt++ {
			var book *ruleBook
			if book, err = r.read(ctx); err != nil {
				return err
			}
			if err = change(book); err != nil {
				return err
			}
			if err = r.apply(ctx, book); !raced(err) {
				return err
			}
		}
		return fmt.Errorf("ses: receipt rules kept changing: %w", err)
	})
}

func raced(err error) bool {
	var exists *sesv1types.AlreadyExistsException
	var missing *sesv1types.RuleDoesNotExistException
	return errors.As(err, &exists) || errors.As(err, &missing)
}

type replyRule struct {
	name       string
	recipients []string
	created    bool
	changed    bool
}

type ruleBook struct {
	set        string
	rules      []*replyRule
	legacy     []string
	foreign    int
	dropLegacy bool
}

func (r *InboundRoutes) read(ctx context.Context) (*ruleBook, error) {
	active, err := r.api.DescribeActiveReceiptRuleSet(ctx, &sesv1.DescribeActiveReceiptRuleSetInput{})
	if err != nil {
		return nil, fmt.Errorf("ses: describe active rule set: %w", err)
	}
	if active == nil || active.Metadata == nil || aws.ToString(active.Metadata.Name) == "" {
		return nil, ErrNoActiveRuleSet
	}
	book := &ruleBook{set: aws.ToString(active.Metadata.Name)}
	for _, rule := range active.Rules {
		name := aws.ToString(rule.Name)
		switch {
		case strings.HasPrefix(name, managedRulePrefix):
			book.rules = append(book.rules, &replyRule{name: name, recipients: slices.Clone(rule.Recipients)})
		case strings.HasPrefix(name, legacyRulePrefix):
			book.legacy = append(book.legacy, name)
		default:
			book.foreign++
		}
	}
	return book, nil
}

func (b *ruleBook) has(recipient string) bool {
	for _, rule := range b.rules {
		if slices.Contains(rule.recipients, recipient) {
			return true
		}
	}
	return false
}

func (b *ruleBook) add(recipients ...string) error {
	for _, recipient := range recipients {
		if b.has(recipient) {
			continue
		}
		rule, err := b.ruleWithRoom()
		if err != nil {
			return err
		}
		rule.recipients = append(rule.recipients, recipient)
		rule.changed = true
	}
	return nil
}

func (b *ruleBook) ruleWithRoom() (*replyRule, error) {
	for _, rule := range b.rules {
		if len(rule.recipients) < emailsending.InboundConditionsPerRule {
			return rule, nil
		}
	}
	if len(b.rules)+len(b.legacy)+b.foreign >= emailsending.InboundRulesPerRuleSet {
		return nil, emailsending.ErrInboundCapacityReached
	}
	rule := &replyRule{name: b.freeName(), created: true}
	b.rules = append(b.rules, rule)
	return rule, nil
}

func (b *ruleBook) freeName() string {
	taken := make(map[string]bool, len(b.rules))
	for _, rule := range b.rules {
		taken[rule.name] = true
	}
	for i := 0; ; i++ {
		if name := fmt.Sprintf("%s%03d", managedRulePrefix, i); !taken[name] {
			return name
		}
	}
}

func (b *ruleBook) remove(recipients ...string) {
	for _, rule := range b.rules {
		kept := slices.DeleteFunc(slices.Clone(rule.recipients), func(r string) bool { return slices.Contains(recipients, r) })
		if len(kept) != len(rule.recipients) {
			rule.recipients = kept
			rule.changed = true
		}
	}
}

func (r *InboundRoutes) apply(ctx context.Context, b *ruleBook) error {
	set := aws.String(b.set)
	var emptied []string
	for _, rule := range b.rules {
		switch {
		case !rule.changed:
		case len(rule.recipients) == 0:
			if !rule.created {
				emptied = append(emptied, rule.name)
			}
		case rule.created:
			if _, err := r.api.CreateReceiptRule(ctx, &sesv1.CreateReceiptRuleInput{RuleSetName: set, Rule: r.receiptRule(rule)}); err != nil {
				return fmt.Errorf("ses: create receipt rule %s: %w", rule.name, err)
			}
		default:
			if _, err := r.api.UpdateReceiptRule(ctx, &sesv1.UpdateReceiptRuleInput{RuleSetName: set, Rule: r.receiptRule(rule)}); err != nil {
				return fmt.Errorf("ses: update receipt rule %s: %w", rule.name, err)
			}
		}
	}
	if b.dropLegacy {
		emptied = append(emptied, b.legacy...)
	}
	for _, name := range emptied {
		if _, err := r.api.DeleteReceiptRule(ctx, &sesv1.DeleteReceiptRuleInput{RuleSetName: set, RuleName: aws.String(name)}); err != nil {
			return fmt.Errorf("ses: delete receipt rule %s: %w", name, err)
		}
	}
	return nil
}

func (r *InboundRoutes) receiptRule(rule *replyRule) *sesv1types.ReceiptRule {
	return &sesv1types.ReceiptRule{
		Name:        aws.String(rule.name),
		Enabled:     true,
		ScanEnabled: true,
		TlsPolicy:   sesv1types.TlsPolicyOptional,
		Recipients:  slices.Clone(rule.recipients),
		Actions: []sesv1types.ReceiptAction{{
			S3Action: &sesv1types.S3Action{
				BucketName:      aws.String(r.cfg.InboundBucket),
				ObjectKeyPrefix: aws.String(emailsending.InboundObjectPrefix),
				TopicArn:        aws.String(r.cfg.InboundTopicARN),
			},
		}},
	}
}

func (r *InboundRoutes) BlockSender(ctx context.Context, ip netip.Addr, until time.Time) (bool, error) {
	if !ip.IsValid() {
		return false, fmt.Errorf("ses: block sender: invalid address")
	}
	ip = ip.Unmap()
	created := false
	err := r.locked(ctx, func() error {
		filters, err := r.filters(ctx)
		if err != nil {
			return err
		}
		if blockedAlready(filters, ip) {
			return nil
		}
		if evict, ok := emailsending.SenderBlockToEvict(managedBlocks(filters), len(filters)); ok {
			if err := r.deleteFilter(ctx, evict.Name); err != nil {
				return err
			}
			log.Printf("[ses] ip filter %s evicted early to make room for a new block", evict.Name)
		} else if len(filters) >= emailsending.InboundIPFiltersPerAccount {
			return ErrFilterQuotaFull
		}
		prefix := netip.PrefixFrom(ip, ip.BitLen())
		if _, err := r.api.CreateReceiptFilter(ctx, &sesv1.CreateReceiptFilterInput{Filter: &sesv1types.ReceiptFilter{
			Name:     aws.String(blockFilterName(ip, until)),
			IpFilter: &sesv1types.ReceiptIpFilter{Cidr: aws.String(prefix.String()), Policy: sesv1types.ReceiptFilterPolicyBlock},
		}}); err != nil {
			return fmt.Errorf("ses: create ip filter: %w", err)
		}
		created = true
		return nil
	})
	return created, err
}

func (r *InboundRoutes) UnblockExpired(ctx context.Context, now time.Time) error {
	return r.locked(ctx, func() error {
		filters, err := r.filters(ctx)
		if err != nil {
			return err
		}
		for _, block := range emailsending.ExpiredSenderBlocks(managedBlocks(filters), now) {
			if err := r.deleteFilter(ctx, block.Name); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *InboundRoutes) IPFilterCount(ctx context.Context) (int, error) {
	filters, err := r.filters(ctx)
	if err != nil {
		return 0, err
	}
	return len(filters), nil
}

func (r *InboundRoutes) filters(ctx context.Context) ([]sesv1types.ReceiptFilter, error) {
	out, err := r.api.ListReceiptFilters(ctx, &sesv1.ListReceiptFiltersInput{})
	if err != nil {
		return nil, fmt.Errorf("ses: list ip filters: %w", err)
	}
	return out.Filters, nil
}

func (r *InboundRoutes) deleteFilter(ctx context.Context, name string) error {
	if _, err := r.api.DeleteReceiptFilter(ctx, &sesv1.DeleteReceiptFilterInput{FilterName: aws.String(name)}); err != nil {
		return fmt.Errorf("ses: delete ip filter %s: %w", name, err)
	}
	return nil
}

func blockedAlready(filters []sesv1types.ReceiptFilter, ip netip.Addr) bool {
	for _, f := range filters {
		if f.IpFilter == nil || f.IpFilter.Policy != sesv1types.ReceiptFilterPolicyBlock {
			continue
		}
		if prefix, ok := filterPrefix(aws.ToString(f.IpFilter.Cidr)); ok && prefix.Contains(ip) {
			return true
		}
	}
	return false
}

func filterPrefix(cidr string) (netip.Prefix, bool) {
	if prefix, err := netip.ParsePrefix(cidr); err == nil {
		return prefix, true
	}
	addr, err := netip.ParseAddr(cidr)
	if err != nil {
		return netip.Prefix{}, false
	}
	addr = addr.Unmap()
	return netip.PrefixFrom(addr, addr.BitLen()), true
}

func blockFilterName(ip netip.Addr, until time.Time) string {
	raw := ip.Unmap().As16()
	return blockFilterPrefix + strconv.FormatInt(until.Unix(), 10) + "-" + hex.EncodeToString(raw[:])
}

func managedBlocks(filters []sesv1types.ReceiptFilter) []emailsending.SenderBlock {
	var out []emailsending.SenderBlock
	for _, f := range filters {
		name := aws.ToString(f.Name)
		rest, ok := strings.CutPrefix(name, blockFilterPrefix)
		if !ok {
			continue
		}
		stamp, _, _ := strings.Cut(rest, "-")
		unix, err := strconv.ParseInt(stamp, 10, 64)
		if err != nil {
			log.Printf("[ses] ip filter %s has no readable expiry; left in place", name)
			continue
		}
		block := emailsending.SenderBlock{Name: name, Until: time.Unix(unix, 0).UTC()}
		if f.IpFilter != nil {
			block.CIDR = aws.ToString(f.IpFilter.Cidr)
		}
		out = append(out, block)
	}
	return out
}
