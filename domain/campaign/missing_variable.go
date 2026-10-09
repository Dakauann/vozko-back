package campaign

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

const MaxSkipMessage = 500

const (
	skipDetailSeparator   = ":"
	missingSlotsSeparator = ";"
)

type MissingVariable struct {
	Slot   int
	Source BindingSource
}

type MissingVariableCount struct {
	Slot   int           `json:"slot"`
	Source BindingSource `json:"source,omitempty"`
	Count  int           `json:"count"`
}

type MissingVariableError struct {
	Missing []MissingVariable
}

func (e *MissingVariableError) Error() string {
	slots := make([]string, 0, len(e.Missing))
	for _, m := range e.Missing {
		slots = append(slots, fmt.Sprintf("{{%d}} (%s)", m.Slot, m.Source))
	}
	return fmt.Sprintf("%v: %s", ErrMissingVariable, strings.Join(slots, ", "))
}

func (e *MissingVariableError) Unwrap() error {
	return ErrMissingVariable
}

func MissingVariablesOf(err error) ([]MissingVariable, bool) {
	var missing *MissingVariableError
	if !errors.As(err, &missing) || missing == nil {
		return nil, false
	}
	known := make([]MissingVariable, 0, len(missing.Missing))
	for _, m := range missing.Missing {
		if m.known() {
			known = append(known, m)
		}
	}
	if len(known) == 0 {
		return nil, false
	}
	return known, true
}

func (m MissingVariable) known() bool {
	return m.Slot > 0
}

func (m MissingVariable) slotText() string {
	return strconv.Itoa(m.Slot)
}

func (m MissingVariable) itemText() string {
	source := strings.TrimSpace(string(m.Source))
	if source == "" || strings.Contains(source, missingSlotsSeparator) {
		return m.slotText()
	}
	return m.slotText() + skipDetailSeparator + source
}

func MissingVariablesMessage(missing []MissingVariable) string {
	known := sortedKnown(missing)
	prefix := string(SkipMissingVariable)
	if len(known) == 0 {
		return prefix
	}
	if full := joinedItems(known, MissingVariable.itemText); len(prefix)+len(skipDetailSeparator)+len(full) <= MaxSkipMessage {
		return prefix + skipDetailSeparator + full
	}
	message := prefix + skipDetailSeparator + known[0].slotText()
	for _, m := range known[1:] {
		next := message + missingSlotsSeparator + m.slotText()
		if len(next) > MaxSkipMessage {
			break
		}
		message = next
	}
	return message
}

func sortedKnown(missing []MissingVariable) []MissingVariable {
	known := make([]MissingVariable, 0, len(missing))
	seen := map[int]bool{}
	for _, m := range missing {
		if m.known() && !seen[m.Slot] {
			seen[m.Slot] = true
			known = append(known, m)
		}
	}
	slices.SortFunc(known, func(a, b MissingVariable) int { return cmp.Compare(a.Slot, b.Slot) })
	return known
}

func joinedItems(missing []MissingVariable, text func(MissingVariable) string) string {
	items := make([]string, 0, len(missing))
	for _, m := range missing {
		items = append(items, text(m))
	}
	return strings.Join(items, missingSlotsSeparator)
}

func ParseMissingVariables(message string) ([]MissingVariable, bool) {
	rest, found := strings.CutPrefix(message, string(SkipMissingVariable)+skipDetailSeparator)
	if !found {
		return nil, false
	}
	items := strings.Split(rest, missingSlotsSeparator)
	missing := make([]MissingVariable, 0, len(items))
	for _, item := range items {
		m, ok := parseMissingItem(item)
		if !ok || (len(missing) > 0 && m.Slot <= missing[len(missing)-1].Slot) {
			return nil, false
		}
		missing = append(missing, m)
	}
	return missing, true
}

func parseMissingItem(item string) (MissingVariable, bool) {
	slotText, source, hasSource := strings.Cut(item, skipDetailSeparator)
	slot, err := strconv.Atoi(slotText)
	if err != nil || slot <= 0 {
		return MissingVariable{}, false
	}
	if hasSource && strings.TrimSpace(source) == "" {
		return MissingVariable{}, false
	}
	return MissingVariable{Slot: slot, Source: BindingSource(source)}, true
}

type SkipDetail struct {
	Missing      []MissingVariable
	CooldownDays int
}

func (r SkipReason) OutcomeWith(detail SkipDetail) (SendStatus, int, string) {
	status, code, message := r.Outcome()
	switch r {
	case SkipMissingVariable:
		message = MissingVariablesMessage(detail.Missing)
	case SkipCooldown:
		if detail.CooldownDays > 0 {
			message = string(SkipCooldown) + skipDetailSeparator + strconv.Itoa(detail.CooldownDays)
		}
	}
	return status, code, message
}

func ParseSkipDetail(reason SkipReason, message string) SkipDetail {
	switch reason {
	case SkipMissingVariable:
		missing, _ := ParseMissingVariables(message)
		return SkipDetail{Missing: missing}
	case SkipCooldown:
		text, found := strings.CutPrefix(message, string(SkipCooldown)+skipDetailSeparator)
		if days, err := strconv.Atoi(text); found && err == nil && days > 0 {
			return SkipDetail{CooldownDays: days}
		}
	}
	return SkipDetail{}
}

func DetailedSkipCodes() []int {
	return []int{SkipCooldown.FailureCode(), SkipMissingVariable.FailureCode()}
}

func missingVariableCounts(byVariable map[MissingVariable]int) []MissingVariableCount {
	counts := make([]MissingVariableCount, 0, len(byVariable))
	for missing, n := range byVariable {
		if n > 0 && missing.known() {
			counts = append(counts, MissingVariableCount{Slot: missing.Slot, Source: missing.Source, Count: n})
		}
	}
	slices.SortFunc(counts, func(a, b MissingVariableCount) int {
		return cmp.Or(cmp.Compare(a.Slot, b.Slot), cmp.Compare(a.Source, b.Source))
	})
	return counts
}
