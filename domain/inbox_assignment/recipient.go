package inbox_assignment

import (
	"cmp"
	"errors"
	"slices"
	"strings"
)

const (
	MinRecipientTier   = 1
	MaxRecipientTier   = 10
	MinRecipientWeight = 1
	MaxRecipientWeight = 10
	MaxRecipients      = 500

	MaxOpenPerMember = 500
)

var (
	ErrInvalidStrategy    = errors.New("roulette: unknown distribution strategy")
	ErrRecipientInvalid   = errors.New("roulette: each recipient needs a member, a tier from 1 to 10 and a weight from 1 to 10")
	ErrRecipientDuplicate = errors.New("roulette: a member appears more than once in the recipient list")
	ErrTooManyRecipients  = errors.New("roulette: the recipient list is longer than allowed")
	ErrRecipientsRequired = errors.New("roulette: this strategy needs at least one recipient")
	ErrRecipientNotMember = errors.New("roulette: a recipient is not a member of this workspace")
	ErrInvalidCapacity    = errors.New("roulette: the open conversation limit must be between 0 and 500")
	roulettePolicyErrors  = []error{ErrInvalidStrategy, ErrRecipientInvalid, ErrRecipientDuplicate, ErrTooManyRecipients, ErrRecipientsRequired, ErrRecipientNotMember, ErrInvalidCapacity}
)

type Recipient struct {
	UserID string `json:"userId"`
	Tier   int    `json:"tier"`
	Weight int    `json:"weight"`
}

func (r Recipient) valid() bool {
	return r.UserID != "" &&
		r.Tier >= MinRecipientTier && r.Tier <= MaxRecipientTier &&
		r.Weight >= MinRecipientWeight && r.Weight <= MaxRecipientWeight
}

func NormalizeRecipients(in []Recipient) ([]Recipient, error) {
	if len(in) > MaxRecipients {
		return nil, ErrTooManyRecipients
	}
	out := make([]Recipient, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, r := range in {
		r.UserID = strings.TrimSpace(r.UserID)
		if !r.valid() {
			return nil, ErrRecipientInvalid
		}
		if seen[r.UserID] {
			return nil, ErrRecipientDuplicate
		}
		seen[r.UserID] = true
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b Recipient) int {
		return cmp.Or(cmp.Compare(a.Tier, b.Tier), cmp.Compare(a.UserID, b.UserID))
	})
	return out, nil
}

func ValidateRouletteRules(strategy Strategy, recipients []Recipient, maxOpenPerMember int) error {
	if maxOpenPerMember < 0 || maxOpenPerMember > MaxOpenPerMember {
		return ErrInvalidCapacity
	}
	if strategy.ChoosesFromRecipients() && len(recipients) == 0 {
		return ErrRecipientsRequired
	}
	return nil
}

func IsRoulettePolicyError(err error) bool {
	for _, policy := range roulettePolicyErrors {
		if errors.Is(err, policy) {
			return true
		}
	}
	return false
}
