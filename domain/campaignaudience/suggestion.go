package campaignaudience

import (
	"cmp"
	"fmt"
	"slices"

	"vozko/domain/campaign"
	"vozko/domain/customfield"
)

type Suggestion struct {
	Slot   int                    `json:"slot"`
	Source campaign.BindingSource `json:"source"`
}

func ValidateSuggestions(suggestions []Suggestion, slots int, defs []*customfield.Definition) ([]Suggestion, error) {
	seen := map[int]bool{}
	out := make([]Suggestion, 0, len(suggestions))
	for _, s := range suggestions {
		if s.Slot < 1 || s.Slot > slots {
			return nil, fmt.Errorf("%w: {{%d}}", ErrSuggestionSlot, s.Slot)
		}
		if seen[s.Slot] {
			return nil, fmt.Errorf("%w: {{%d}}", ErrSuggestionDuplicate, s.Slot)
		}
		seen[s.Slot] = true
		v := Variable{Source: s.Source}.normalized()
		if v.Source == campaign.BindLiteral {
			return nil, ErrSuggestionLiteral
		}
		if err := campaign.ValidateBindingSource(v.Source, defs); err != nil {
			return nil, fmt.Errorf("variable {{%d}}: %w", s.Slot, err)
		}
		out = append(out, Suggestion{Slot: s.Slot, Source: v.Source})
	}
	slices.SortFunc(out, func(a, b Suggestion) int { return cmp.Compare(a.Slot, b.Slot) })
	return out, nil
}

func SuggestedSlots(slots int, suggestions []Suggestion) []Slot {
	bySlot := map[int]campaign.BindingSource{}
	for _, s := range suggestions {
		bySlot[s.Slot] = s.Source
	}
	out := make([]Slot, 0, slots)
	for n := 1; n <= slots; n++ {
		out = append(out, Slot{Key: SlotKey(n), Suggested: bySlot[n]})
	}
	return out
}

func SlotKey(n int) string {
	return fmt.Sprintf("%d", n)
}
