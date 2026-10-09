package selection

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"vozko/domain/crmfilter"
)

type Mode string

const (
	ModeIDs         Mode = "ids"
	ModeAllMatching Mode = "all_matching"
	ModeFirstN      Mode = "first_n"
	ModeEveryone    Mode = "everyone"
)

const (
	MaxExplicitIDs = 5000
	MaxFirstN      = 200000
	MaxResolvePage = MaxExplicitIDs + 1
)

const fingerprintVersion = "selection.v1"

var (
	ErrUnknownMode         = errors.New("selection: exactly one known mode is required")
	ErrEmptySelection      = errors.New("selection: no ids were picked")
	ErrAmbiguousSelection  = errors.New("selection: fields of another mode were sent")
	ErrTooManyIDs          = errors.New("selection: too many picked ids, select by filter instead")
	ErrFilterRequired      = errors.New("selection: a non-empty filter is required, the whole base is only selected as everyone")
	ErrLimitRequired       = errors.New("selection: first_n needs a positive limit within the cap")
	ErrEveryoneUnconfirmed = errors.New("selection: everyone must be confirmed with the expected count")
	ErrFingerprintMismatch = errors.New("selection: the count was taken on a different filter")
	ErrInvalidCount        = errors.New("selection: expected count cannot be negative")
	ErrCountChanged        = errors.New("selection: the selection changed since it was counted")
	ErrModeUnsupported     = errors.New("selection: this mode is not supported here")
	ErrResolverUnavailable = errors.New("selection: the selection cannot be resolved here")
	ErrInvalidPage         = errors.New("selection: resolve page size must be between 1 and the page cap")
	ErrScopeDenied         = errors.New("selection: the actor cannot reach this scope")
	ErrInvalidIDs          = errors.New("selection: every picked id must be a valid record id")
	ErrUnknownSort         = errors.New("selection: the order of a quantity selection uses an unknown sort key")
	ErrCountRequired       = errors.New("selection: a filtered selection must be confirmed with the count it was shown")
)

type Selection struct {
	Mode          Mode
	IDs           []string
	Filter        *crmfilter.Filter
	Sort          []crmfilter.Sort
	Limit         int
	ExcludeIDs    []string
	ExpectedCount int
	Fingerprint   string
	Require       *crmfilter.Filter
}

type Scope struct {
	WorkspaceID, ActorID, DepartmentID string
	IsAdmin                            bool
}

type Ref struct{ ID, Type string }

type Resolver interface {
	Count(ctx context.Context, scope Scope, s Selection) (int, error)
	Resolve(ctx context.Context, scope Scope, s Selection, after string, limit int) ([]Ref, error)
}

type SkipReason string

type Failure struct {
	ID    string `json:"id"`
	Error string `json:"error"`
}

type Result struct {
	Matched   int                `json:"matched"`
	Eligible  int                `json:"eligible"`
	Succeeded int                `json:"succeeded"`
	Skipped   map[SkipReason]int `json:"skipped,omitempty"`
	Failed    []Failure          `json:"failed"`
	Truncated bool               `json:"truncated"`
}

func (r *Result) Fail(id string, err error) {
	r.Failed = append(r.Failed, Failure{ID: id, Error: err.Error()})
}

func (r *Result) Skip(reason SkipReason) {
	if r.Skipped == nil {
		r.Skipped = map[SkipReason]int{}
	}
	r.Skipped[reason]++
}

type CountChangedError struct {
	Expected int
	Matched  int
}

func (e *CountChangedError) Error() string {
	return fmt.Sprintf("%s: expected %d, now %d", ErrCountChanged.Error(), e.Expected, e.Matched)
}

func (e *CountChangedError) Is(target error) bool { return target == ErrCountChanged }

func (s Selection) Validate() error {
	return s.validate(true)
}

func (s Selection) ValidateConfirmed() error {
	if err := s.Validate(); err != nil {
		return err
	}
	if s.Mode != ModeIDs && s.ExpectedCount == 0 {
		return ErrCountRequired
	}
	return nil
}

func (s Selection) ValidateForCount() error {
	return s.validate(false)
}

func (s Selection) validate(confirmed bool) error {
	if s.ExpectedCount < 0 {
		return ErrInvalidCount
	}
	if len(s.ExcludeIDs) > MaxExplicitIDs {
		return ErrTooManyIDs
	}
	if s.Require != nil {
		if err := s.Require.ValidateForSelection(); err != nil {
			return err
		}
	}
	switch s.Mode {
	case ModeIDs:
		return s.validateIDs()
	case ModeAllMatching:
		return s.validateFiltered(false, confirmed)
	case ModeFirstN:
		return s.validateFiltered(true, confirmed)
	case ModeEveryone:
		return s.validateEveryone(confirmed)
	default:
		return fmt.Errorf("%w: %q", ErrUnknownMode, s.Mode)
	}
}

func (s Selection) ConfirmableCount(matched int) int {
	switch s.Mode {
	case ModeIDs:
		return 0
	case ModeFirstN:
		return min(s.Limit, matched)
	default:
		return matched
	}
}

func (s Selection) Exceeds(matched, max int) bool {
	return s.ConfirmableCount(matched)-len(s.ExcludeIDs) > max
}

func (s Selection) EffectiveFilter() crmfilter.Filter {
	if s.Mode == ModeEveryone || s.Filter == nil {
		return crmfilter.Filter{}
	}
	return *s.Filter
}

func (s Selection) ConfirmCount(matched int) error {
	if s.Mode == ModeIDs || s.ExpectedCount == 0 {
		return nil
	}
	if s.ConfirmableCount(matched) != s.ExpectedCount {
		return &CountChangedError{Expected: s.ExpectedCount, Matched: matched}
	}
	return nil
}

func (s Selection) validateIDs() error {
	if s.Filter != nil || s.Limit != 0 || len(s.ExcludeIDs) > 0 {
		return ErrAmbiguousSelection
	}
	if len(s.IDs) > MaxExplicitIDs {
		return ErrTooManyIDs
	}
	for _, id := range s.IDs {
		if strings.TrimSpace(id) == "" {
			return ErrEmptySelection
		}
	}
	if len(s.IDs) == 0 {
		return ErrEmptySelection
	}
	return nil
}

func (s Selection) validateFiltered(limited, confirmed bool) error {
	if len(s.IDs) > 0 || (!limited && s.Limit != 0) {
		return ErrAmbiguousSelection
	}
	if s.Filter == nil || s.Filter.IsEmpty() {
		return ErrFilterRequired
	}
	if err := s.Filter.ValidateForSelection(); err != nil {
		return err
	}
	if limited && (s.Limit <= 0 || s.Limit > MaxFirstN) {
		return ErrLimitRequired
	}
	if !confirmed {
		return nil
	}
	return s.matchesFingerprint()
}

func (s Selection) validateEveryone(confirmed bool) error {
	if len(s.IDs) > 0 || s.Limit != 0 || (s.Filter != nil && !s.Filter.IsEmpty()) {
		return ErrAmbiguousSelection
	}
	if !confirmed {
		return nil
	}
	if s.ExpectedCount == 0 {
		return ErrEveryoneUnconfirmed
	}
	return s.matchesFingerprint()
}

func (s Selection) matchesFingerprint() error {
	if s.Fingerprint == "" || s.Fingerprint != Fingerprint(s.EffectiveFilter()) {
		return ErrFingerprintMismatch
	}
	return nil
}

func Fingerprint(f crmfilter.Filter) string {
	canonical := crmfilter.Filter{Groups: make([]crmfilter.Group, 0, len(f.Groups))}
	for _, g := range f.Groups {
		if len(g.Predicates) == 0 {
			continue
		}
		canonical.Groups = append(canonical.Groups, crmfilter.Group{Conjunction: g.Conj(), Predicates: g.Predicates})
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		encoded = []byte(err.Error())
	}
	sum := sha256.Sum256(append([]byte(fingerprintVersion+"|"), encoded...))
	return hex.EncodeToString(sum[:])
}

func ValidatePage(limit int) error {
	if limit < 1 || limit > MaxResolvePage {
		return ErrInvalidPage
	}
	return nil
}
