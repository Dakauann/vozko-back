package lead

import (
	"errors"
	"strings"

	"vozko/domain/shared"
)

type DialPurpose string

const (
	DialDirect   DialPurpose = "direct"
	DialCallList DialPurpose = "call_list"
)

type DialContext struct {
	Purpose DialPurpose
}

type DialRefusalReason string

const (
	DialRefusedNotFound       DialRefusalReason = "not_found"
	DialRefusedBlocked        DialRefusalReason = "blocked"
	DialRefusedNumberNotHeld  DialRefusalReason = "number_not_held"
	DialRefusedNoNumber       DialRefusalReason = "no_number"
	DialRefusedOptedOut       DialRefusalReason = "opted_out"
	DialRefusedUnknownPurpose DialRefusalReason = "unknown_purpose"
	DialRefusedInvalidNumber  DialRefusalReason = "invalid_number"
)

var (
	ErrLeadNotDialable = errors.New("lead: this number of the lead cannot be called")
	ErrLeadDialBlocked = errors.New("lead: blocked, calls are refused")
)

type DialRefusal struct {
	Reason DialRefusalReason
}

func (r *DialRefusal) Error() string {
	return "lead: call refused (" + string(r.Reason) + ")"
}

func (r *DialRefusal) Unwrap() error {
	if r.Reason == DialRefusedBlocked {
		return ErrLeadDialBlocked
	}
	return ErrLeadNotDialable
}

func refusalError(reason DialRefusalReason) error {
	if reason == "" {
		return nil
	}
	return &DialRefusal{Reason: reason}
}

func CheckDial(l *Lead, number string, c DialContext) error {
	return refusalError(dialRefusal(l, number, c))
}

func CheckLeadDial(l *Lead, number string, identities []*Lead, c DialContext) error {
	return refusalError(leadDialRefusal(l, number, identities, c))
}

func CheckNumberDial(number string, identities []*Lead, c DialContext) (*Lead, error) {
	holder := IdentityOfNumber(number, identities)
	if holder == nil {
		return nil, refusalError(purposeRefusal(c))
	}
	if err := CheckDial(holder, number, c); err != nil {
		return nil, err
	}
	return holder, nil
}

func IdentityOfNumber(number string, leads []*Lead) *Lead {
	for _, l := range leads {
		if l != nil && l.HoldsIdentity(number) {
			return l
		}
	}
	return nil
}

func leadDialRefusal(l *Lead, number string, identities []*Lead, c DialContext) DialRefusalReason {
	if reason := dialRefusal(l, number, c); reason != "" {
		return reason
	}
	holder := IdentityOfNumber(number, identities)
	if holder == nil || holder.ID == l.ID {
		return ""
	}
	return dialRefusal(holder, number, c)
}

func purposeRefusal(c DialContext) DialRefusalReason {
	if c.Purpose != DialDirect && c.Purpose != DialCallList {
		return DialRefusedUnknownPurpose
	}
	return ""
}

func dialRefusal(l *Lead, number string, c DialContext) DialRefusalReason {
	if l == nil {
		return DialRefusedNotFound
	}
	if reason := purposeRefusal(c); reason != "" {
		return reason
	}
	if l.Blocked {
		return DialRefusedBlocked
	}
	if !l.HoldsNumber(number) {
		return DialRefusedNumberNotHeld
	}
	if l.OptedOutAt != nil && c.Purpose == DialCallList {
		return DialRefusedOptedOut
	}
	return ""
}

type DialNumber struct {
	Number   string
	Identity bool
	Label    PhoneLabel
	PhoneID  string
}

func (l *Lead) DialNumbers() []DialNumber {
	numbers := make([]DialNumber, 0, len(l.Phones)+1)
	seen := map[string]bool{}
	add := func(number string, identity bool, label PhoneLabel, phoneID string) {
		dialable := shared.EnsureDialablePhoneNumber(number)
		if strings.TrimSpace(dialable) == "" || seen[dialable] {
			return
		}
		for _, format := range NumberFormats(dialable) {
			seen[format] = true
		}
		seen[dialable] = true
		numbers = append(numbers, DialNumber{Number: dialable, Identity: identity, Label: label, PhoneID: phoneID})
	}
	if l.HasIdentity() {
		add(l.Number, true, "", "")
	}
	for _, phone := range l.Phones {
		add(phone.Number, false, phone.Label, phone.ID)
	}
	return numbers
}

type PlannedDialNumber struct {
	DialNumber
	Refusal DialRefusalReason
}

type DialPlan struct {
	Numbers  []PlannedDialNumber
	Refusal  DialRefusalReason
	Callable string
}

func (l *Lead) PlanDial(identities []*Lead, c DialContext) DialPlan {
	plan := DialPlan{Numbers: []PlannedDialNumber{}}
	for _, number := range l.DialNumbers() {
		plan.Numbers = append(plan.Numbers, PlannedDialNumber{DialNumber: number, Refusal: leadDialRefusal(l, number.Number, identities, c)})
	}
	plan.settle()
	return plan
}

func (p *DialPlan) Refuse(number string, reason DialRefusalReason) {
	for i := range p.Numbers {
		if p.Numbers[i].Number == number && p.Numbers[i].Refusal == "" {
			p.Numbers[i].Refusal = reason
		}
	}
	p.settle()
}

func (p *DialPlan) settle() {
	p.Callable, p.Refusal = "", ""
	for _, number := range p.Numbers {
		if number.Refusal == "" {
			p.Callable = number.Number
			return
		}
	}
	if len(p.Numbers) == 0 {
		p.Refusal = DialRefusedNoNumber
		return
	}
	p.Refusal = p.Numbers[0].Refusal
}
