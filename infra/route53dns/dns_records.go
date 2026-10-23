package route53dns

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	"github.com/aws/aws-sdk-go-v2/service/route53/types"
	"github.com/aws/smithy-go/middleware"

	"vozko/domain/emailsending"
)

const (
	DefaultTTL       = 300
	CallSpacing      = 250 * time.Millisecond
	CallSlotKey      = "route53:calls:slot"
	CallWantedPrefix = "route53:calls:wanted:"
	ServiceName      = "Route 53"
	callAttempts     = 3
	changeAttempts   = 3
	maxTXTString     = 255
	zoneIDPrefix     = "/hostedzone/"
)

var (
	ErrConfig            = errors.New("route53 dns: incomplete configuration")
	ErrUnknownZone       = errors.New("route53 dns: no hosted zone for the shared domain")
	ErrZoneMismatch      = errors.New("route53 dns: the hosted zone does not serve the shared domain")
	ErrRecordOutsideZone = errors.New("route53 dns: record outside the shared domain")
	ErrUnsupportedRecord = errors.New("route53 dns: unsupported record type")
	ErrChangeConflict    = errors.New("route53 dns: the record sets kept changing while being removed")
)

var supportedTypes = map[string]types.RRType{"CNAME": types.RRTypeCname, "MX": types.RRTypeMx, "TXT": types.RRTypeTxt}

type API interface {
	ChangeResourceRecordSets(ctx context.Context, in *route53.ChangeResourceRecordSetsInput, opts ...func(*route53.Options)) (*route53.ChangeResourceRecordSetsOutput, error)
	ListResourceRecordSets(ctx context.Context, in *route53.ListResourceRecordSetsInput, opts ...func(*route53.Options)) (*route53.ListResourceRecordSetsOutput, error)
	GetHostedZone(ctx context.Context, in *route53.GetHostedZoneInput, opts ...func(*route53.Options)) (*route53.GetHostedZoneOutput, error)
}

func EveryCall(string) bool { return true }

func NewClient(cfg aws.Config, endpoint string, pace func(*middleware.Stack) error) (*route53.Client, error) {
	if pace == nil {
		return nil, fmt.Errorf("%w: call pacing", ErrConfig)
	}
	endpoint = strings.TrimSpace(endpoint)
	return route53.NewFromConfig(cfg, func(o *route53.Options) {
		o.RetryMode = aws.RetryModeStandard
		o.RetryMaxAttempts = callAttempts
		o.APIOptions = append(o.APIOptions, pace)
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
	}), nil
}

type Config struct {
	Zones map[string]string
}

type DNSRecords struct {
	api   API
	zones map[string]string

	mu      sync.Mutex
	checked map[string]bool
}

var _ emailsending.SubdomainDNS = (*DNSRecords)(nil)

func New(api API, cfg Config) (*DNSRecords, error) {
	if api == nil || len(cfg.Zones) == 0 {
		return nil, ErrConfig
	}
	zones := make(map[string]string, len(cfg.Zones))
	for domain, zone := range cfg.Zones {
		d := canonical(domain)
		id := strings.TrimPrefix(strings.TrimSpace(zone), zoneIDPrefix)
		if d == "" || id == "" {
			return nil, ErrConfig
		}
		zones[d] = id
	}
	return &DNSRecords{api: api, zones: zones, checked: map[string]bool{}}, nil
}

func (d *DNSRecords) UpsertRecords(ctx context.Context, sharedDomain string, records []emailsending.DNSRecord) error {
	zone, sets, err := d.prepare(ctx, sharedDomain, records)
	if err != nil || len(sets) == 0 {
		return err
	}
	changes := make([]types.Change, 0, len(sets))
	for _, set := range sets {
		changes = append(changes, types.Change{Action: types.ChangeActionUpsert, ResourceRecordSet: &set})
	}
	return d.change(ctx, zone, changes)
}

func (d *DNSRecords) DeleteRecords(ctx context.Context, sharedDomain string, records []emailsending.DNSRecord) error {
	zone, gone, err := d.prepare(ctx, sharedDomain, records)
	if err != nil {
		return err
	}
	var conflict error
	for range changeAttempts {
		changes, err := d.removals(ctx, zone, gone)
		if err != nil || len(changes) == 0 {
			return err
		}
		err = d.change(ctx, zone, changes)
		var invalid *types.InvalidChangeBatch
		if !errors.As(err, &invalid) {
			return err
		}
		conflict = err
	}
	return fmt.Errorf("%w: %v", ErrChangeConflict, conflict)
}

func (d *DNSRecords) prepare(ctx context.Context, sharedDomain string, records []emailsending.DNSRecord) (string, []types.ResourceRecordSet, error) {
	shared := canonical(sharedDomain)
	zone, ok := d.zones[shared]
	if !ok {
		return "", nil, fmt.Errorf("%w: %q", ErrUnknownZone, sharedDomain)
	}
	sets, err := recordSets(shared, records)
	if err != nil {
		return "", nil, err
	}
	if err := d.checkZone(ctx, zone, shared); err != nil {
		return "", nil, err
	}
	return zone, sets, nil
}

func (d *DNSRecords) checkZone(ctx context.Context, zone, shared string) error {
	d.mu.Lock()
	done := d.checked[zone]
	d.mu.Unlock()
	if done {
		return nil
	}
	out, err := d.api.GetHostedZone(ctx, &route53.GetHostedZoneInput{Id: aws.String(zone)})
	if err != nil {
		return fmt.Errorf("route53 dns: read hosted zone %s: %w", zone, err)
	}
	name := ""
	if out.HostedZone != nil {
		name = canonical(aws.ToString(out.HostedZone.Name))
	}
	if name == "" || (shared != name && !strings.HasSuffix(shared, "."+name)) {
		return fmt.Errorf("%w: zone %s is %q, the shared domain is %q", ErrZoneMismatch, zone, name, shared)
	}
	d.mu.Lock()
	d.checked[zone] = true
	d.mu.Unlock()
	return nil
}

func (d *DNSRecords) removals(ctx context.Context, zone string, gone []types.ResourceRecordSet) ([]types.Change, error) {
	var changes []types.Change
	for _, set := range gone {
		current, found, err := d.current(ctx, zone, aws.ToString(set.Name), set.Type)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		kept := without(current.ResourceRecords, set.ResourceRecords, set.Type)
		switch {
		case len(kept) == 0:
			changes = append(changes, types.Change{Action: types.ChangeActionDelete, ResourceRecordSet: &current})
		case len(kept) < len(current.ResourceRecords):
			remaining := current
			remaining.ResourceRecords = kept
			changes = append(changes, types.Change{Action: types.ChangeActionUpsert, ResourceRecordSet: &remaining})
		}
	}
	return changes, nil
}

func (d *DNSRecords) current(ctx context.Context, zone, name string, recordType types.RRType) (types.ResourceRecordSet, bool, error) {
	out, err := d.api.ListResourceRecordSets(ctx, &route53.ListResourceRecordSetsInput{
		HostedZoneId:    aws.String(zone),
		StartRecordName: aws.String(name),
		StartRecordType: recordType,
		MaxItems:        aws.Int32(1),
	})
	if err != nil {
		return types.ResourceRecordSet{}, false, fmt.Errorf("route53 dns: read %s %s: %w", recordType, name, err)
	}
	for _, set := range out.ResourceRecordSets {
		if canonical(aws.ToString(set.Name)) == canonical(name) && set.Type == recordType && set.SetIdentifier == nil {
			return set, true, nil
		}
	}
	return types.ResourceRecordSet{}, false, nil
}

func (d *DNSRecords) change(ctx context.Context, zone string, changes []types.Change) error {
	_, err := d.api.ChangeResourceRecordSets(ctx, &route53.ChangeResourceRecordSetsInput{
		HostedZoneId: aws.String(zone),
		ChangeBatch:  &types.ChangeBatch{Changes: changes},
	})
	if err != nil {
		return fmt.Errorf("route53 dns: change record sets of zone %s: %w", zone, err)
	}
	return nil
}

func recordSets(shared string, records []emailsending.DNSRecord) ([]types.ResourceRecordSet, error) {
	var sets []types.ResourceRecordSet
	index := map[string]int{}
	for _, rec := range records {
		name := canonical(rec.Name)
		if !strings.HasSuffix(name, "."+shared) {
			return nil, fmt.Errorf("%w: %q", ErrRecordOutsideZone, rec.Name)
		}
		recordType, ok := supportedTypes[strings.ToUpper(strings.TrimSpace(rec.Type))]
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrUnsupportedRecord, rec.Type)
		}
		value := recordValue(recordType, rec)
		key := string(recordType) + " " + name
		at, seen := index[key]
		if !seen {
			index[key] = len(sets)
			sets = append(sets, types.ResourceRecordSet{Name: aws.String(name + "."), Type: recordType, TTL: aws.Int64(DefaultTTL)})
			at = len(sets) - 1
		}
		if rec.TTL > 0 && (!seen || int64(rec.TTL) > aws.ToInt64(sets[at].TTL)) {
			sets[at].TTL = aws.Int64(int64(rec.TTL))
		}
		if !slices.ContainsFunc(sets[at].ResourceRecords, func(r types.ResourceRecord) bool { return aws.ToString(r.Value) == value }) {
			sets[at].ResourceRecords = append(sets[at].ResourceRecords, types.ResourceRecord{Value: aws.String(value)})
		}
	}
	return sets, nil
}

func recordValue(recordType types.RRType, rec emailsending.DNSRecord) string {
	switch recordType {
	case types.RRTypeMx:
		return strconv.Itoa(rec.Priority) + " " + strings.TrimSpace(rec.Value)
	case types.RRTypeTxt:
		return txtValue(rec.Value)
	}
	return strings.TrimSpace(rec.Value)
}

func txtValue(value string) string {
	if len(value) >= 2 && strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
		return value
	}
	var parts []string
	for len(value) > maxTXTString {
		parts = append(parts, quote(value[:maxTXTString]))
		value = value[maxTXTString:]
	}
	return strings.Join(append(parts, quote(value)), " ")
}

func quote(s string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
}

func without(current, gone []types.ResourceRecord, recordType types.RRType) []types.ResourceRecord {
	drop := map[string]bool{}
	for _, r := range gone {
		drop[comparable(recordType, aws.ToString(r.Value))] = true
	}
	var kept []types.ResourceRecord
	for _, r := range current {
		if !drop[comparable(recordType, aws.ToString(r.Value))] {
			kept = append(kept, r)
		}
	}
	return kept
}

func comparable(recordType types.RRType, value string) string {
	if recordType == types.RRTypeTxt {
		return value
	}
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(value), "."))
}

func canonical(name string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
}
