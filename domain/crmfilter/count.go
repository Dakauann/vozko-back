package crmfilter

import (
	"fmt"
	"strings"

	"vozko/domain/shared"
)

func (p Predicate) MatchesAnEmptyCount() (bool, error) {
	bounds := make([]float64, 0, len(p.Values))
	for _, v := range p.Values {
		n, err := shared.ParseNumberText(strings.TrimSpace(v))
		if err != nil {
			return false, fmt.Errorf("%w: %q", ErrInvalidNumber, v)
		}
		bounds = append(bounds, n)
	}
	first := func() (float64, error) {
		if len(bounds) == 0 {
			return 0, ErrMissingValue
		}
		return bounds[0], nil
	}
	switch p.Operator {
	case OpIsSet:
		return true, nil
	case OpIsEmpty:
		return false, nil
	case OpBetween:
		if len(bounds) != 2 {
			return false, ErrBetweenValues
		}
		return bounds[0] <= 0 && 0 <= bounds[1], nil
	case OpEquals, OpNotEquals, OpGreaterEq, OpLessEq:
		n, err := first()
		if err != nil {
			return false, err
		}
		switch p.Operator {
		case OpEquals:
			return n == 0, nil
		case OpNotEquals:
			return n != 0, nil
		case OpGreaterEq:
			return 0 >= n, nil
		default:
			return 0 <= n, nil
		}
	}
	return false, fmt.Errorf("%w: %q on a count", ErrUnsupportedOp, p.Operator)
}
