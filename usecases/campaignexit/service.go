package campaignexit_usecase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"time"

	"vozko/domain/cache"
	"vozko/domain/campaign"
	"vozko/domain/lead"
)

const (
	MaxNumbers = campaign.MaxEntries
	DefaultTTL = 60 * time.Second
	keyPrefix  = "campaignexits"
)

var (
	ErrWorkspaceRequired = errors.New("campaign exit: workspace is required")
	ErrTooManyNumbers    = errors.New("campaign exit: the list has more numbers than one campaign takes")
	ErrUnavailable       = errors.New("campaign exit: the audience count is not available on this server")
)

var errorCodes = []struct {
	err  error
	code string
}{
	{ErrWorkspaceRequired, "campaign_exit_workspace_required"},
	{ErrTooManyNumbers, "campaign_exit_too_many_numbers"},
	{ErrUnavailable, "campaign_exit_unavailable"},
}

func ErrorCode(err error) string {
	for _, known := range errorCodes {
		if errors.Is(err, known.err) {
			return known.code
		}
	}
	return ""
}

type ExitFacts interface {
	AudienceExitFacts(ctx context.Context, workspaceID string, numbers []string) ([]campaign.LeadFactsGroup, error)
}

type Deps struct {
	Facts    ExitFacts
	Memo     cache.Memo
	Gate     cache.Gate
	Versions cache.Versions
	TTL      time.Duration
}

type Service struct {
	deps Deps
}

func NewService(deps Deps) (*Service, error) {
	if deps.Facts == nil || deps.Memo == nil || deps.Gate == nil || deps.Versions == nil {
		return nil, ErrUnavailable
	}
	if deps.TTL <= 0 {
		deps.TTL = DefaultTTL
	}
	return &Service{deps: deps}, nil
}

func (s *Service) AudienceExits(ctx context.Context, workspaceID string, numbers []string) (campaign.AudienceExits, error) {
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return campaign.AudienceExits{}, ErrWorkspaceRequired
	}
	if len(numbers) > MaxNumbers {
		return campaign.AudienceExits{}, ErrTooManyNumbers
	}
	list := canonicalList(numbers)
	if len(list) == 0 {
		return campaign.AudienceExits{}, nil
	}
	read := func(ctx context.Context) (campaign.AudienceExits, error) {
		var groups []campaign.LeadFactsGroup
		err := cache.Gated(ctx, s.deps.Gate, func(ctx context.Context) error {
			var err error
			groups, err = s.deps.Facts.AudienceExitFacts(ctx, workspaceID, list)
			return err
		})
		if err != nil {
			return campaign.AudienceExits{}, err
		}
		return campaign.TallyAudienceExits(groups), nil
	}
	generation, err := s.deps.Versions.Version(workspaceID)
	if err != nil {
		return read(ctx)
	}
	return cache.Remember(ctx, s.deps.Memo, keyPrefix+":"+workspaceID+":"+generation+":"+fingerprint(list), s.deps.TTL, read)
}

func canonicalList(numbers []string) []string {
	seen := make(map[string]bool, len(numbers))
	list := make([]string, 0, len(numbers))
	for _, raw := range numbers {
		number := lead.NormalizeNumber(raw)
		if number == "" || seen[number] {
			continue
		}
		seen[number] = true
		list = append(list, number)
	}
	sort.Strings(list)
	return list
}

func fingerprint(list []string) string {
	sum := sha256.Sum256([]byte(strings.Join(list, ",")))
	return hex.EncodeToString(sum[:])
}
