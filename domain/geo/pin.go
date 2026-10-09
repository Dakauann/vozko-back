package geo

import (
	"errors"
	"fmt"
	"time"
)

var ErrPointOutsideBrazil = errors.New("geo: the point is outside Brazil")

func Pinned(p Point, source FixSource, at time.Time) (Fix, error) {
	if !source.Confirmed() {
		return Fix{}, fmt.Errorf("%w: a pin comes from a person or from a location the lead sent, not %q", ErrInvalidFix, source)
	}
	if err := p.Validate(); err != nil {
		return Fix{}, err
	}
	if !p.InBrazil() {
		return Fix{}, ErrPointOutsideBrazil
	}
	return Fix{Point: p, Precision: PrecisionExact, Source: source, FixedAt: at.UTC()}, nil
}
