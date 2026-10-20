package inbox_assignment

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func TestNormalizeRecipients_TrimsAndOrdersByTierThenUser(t *testing.T) {
	got, err := NormalizeRecipients([]Recipient{
		{UserID: " cid ", Tier: 2, Weight: 1},
		{UserID: "bob", Tier: 1, Weight: 4},
		{UserID: "ana", Tier: 2, Weight: 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []Recipient{{UserID: "bob", Tier: 1, Weight: 4}, {UserID: "ana", Tier: 2, Weight: 10}, {UserID: "cid", Tier: 2, Weight: 1}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
}

func TestNormalizeRecipients_EmptyListIsAllowed(t *testing.T) {
	got, err := NormalizeRecipients(nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestNormalizeRecipients_Refusals(t *testing.T) {
	tooMany := make([]Recipient, MaxRecipients+1)
	for i := range tooMany {
		tooMany[i] = Recipient{UserID: fmt.Sprintf("u%d", i), Tier: 1, Weight: 1}
	}
	cases := []struct {
		name string
		in   []Recipient
		want error
	}{
		{"blank user", []Recipient{{UserID: "  ", Tier: 1, Weight: 1}}, ErrRecipientInvalid},
		{"tier zero", []Recipient{{UserID: "ana", Tier: 0, Weight: 1}}, ErrRecipientInvalid},
		{"tier above the last", []Recipient{{UserID: "ana", Tier: MaxRecipientTier + 1, Weight: 1}}, ErrRecipientInvalid},
		{"weight zero", []Recipient{{UserID: "ana", Tier: 1, Weight: 0}}, ErrRecipientInvalid},
		{"weight above the most", []Recipient{{UserID: "ana", Tier: 1, Weight: MaxRecipientWeight + 1}}, ErrRecipientInvalid},
		{"same person twice", []Recipient{{UserID: "ana", Tier: 1, Weight: 1}, {UserID: " ana", Tier: 2, Weight: 1}}, ErrRecipientDuplicate},
		{"too many", tooMany, ErrTooManyRecipients},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NormalizeRecipients(tc.in); !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
		})
	}
}

func TestIsRoulettePolicyError(t *testing.T) {
	for _, err := range []error{ErrInvalidStrategy, ErrRecipientInvalid, ErrRecipientDuplicate, ErrTooManyRecipients, ErrRecipientsRequired, ErrRecipientNotMember, ErrInvalidCapacity} {
		if !IsRoulettePolicyError(fmt.Errorf("wrapped: %w", err)) {
			t.Fatalf("%v must read as a policy error", err)
		}
	}
	if IsRoulettePolicyError(errors.New("database down")) {
		t.Fatal("an infrastructure error is not a policy error")
	}
}

func TestValidateRouletteRules(t *testing.T) {
	list := []Recipient{{UserID: "ana", Tier: 1, Weight: 1}}
	cases := []struct {
		name       string
		strategy   Strategy
		recipients []Recipient
		maxOpen    int
		want       error
	}{
		{"round robin needs no list", StrategyRoundRobin, nil, 0, nil},
		{"priority without a list", StrategyPriority, nil, 0, ErrRecipientsRequired},
		{"weighted without a list", StrategyWeighted, nil, 0, ErrRecipientsRequired},
		{"priority with a list", StrategyPriority, list, 0, nil},
		{"negative capacity", StrategyRoundRobin, nil, -1, ErrInvalidCapacity},
		{"capacity above the most", StrategyRoundRobin, nil, MaxOpenPerMember + 1, ErrInvalidCapacity},
		{"capacity at the most", StrategyLeastBusy, nil, MaxOpenPerMember, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateRouletteRules(tc.strategy, tc.recipients, tc.maxOpen); !errors.Is(err, tc.want) {
				t.Fatalf("got %v want %v", err, tc.want)
			}
		})
	}
}
