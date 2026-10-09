package geo

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"vozko/domain/cep"
)

var ErrInvalidFix = errors.New("geo: invalid fix")

const (
	streetSpreadM     = 300
	postalCodeSpreadM = 1500
)

type Precision string

const (
	PrecisionExact      Precision = "exact"
	PrecisionAddress    Precision = "address"
	PrecisionStreet     Precision = "street"
	PrecisionPostalCode Precision = "postal_code"
	PrecisionDistrict   Precision = "district"
	PrecisionCity       Precision = "city"
)

var precisionsBestFirst = []Precision{PrecisionExact, PrecisionAddress, PrecisionStreet, PrecisionPostalCode, PrecisionDistrict, PrecisionCity}

var precisionRank = rankWorstFirst(precisionsBestFirst)

func rankWorstFirst(bestFirst []Precision) map[Precision]int {
	ranks := make(map[Precision]int, len(bestFirst))
	for i, p := range bestFirst {
		ranks[p] = len(bestFirst) - i
	}
	return ranks
}

func PrecisionsBestFirst() []Precision {
	return append([]Precision(nil), precisionsBestFirst...)
}

func PinningPrecisions() []Precision {
	var pinning []Precision
	for _, p := range PrecisionsBestFirst() {
		if p.PinsAHouse() {
			pinning = append(pinning, p)
		}
	}
	return pinning
}

func (p Precision) Known() bool {
	_, ok := precisionRank[p]
	return ok
}

func (p Precision) PinsAHouse() bool {
	return p.Better(PrecisionPostalCode)
}

func (p Precision) Better(than Precision) bool {
	return p.Known() && precisionRank[p] > precisionRank[than]
}

type FixSource string

const (
	SourceManual    FixSource = "manual"
	SourceLeadPin   FixSource = "lead_pin"
	SourceImport    FixSource = "import"
	SourceReference FixSource = "reference"
	SourceProvider  FixSource = "provider"
)

var knownSources = map[FixSource]bool{
	SourceManual:    true,
	SourceLeadPin:   true,
	SourceImport:    true,
	SourceReference: true,
	SourceProvider:  true,
}

func (s FixSource) Confirmed() bool {
	return s == SourceManual || s == SourceLeadPin
}

type Fix struct {
	Point     Point
	Precision Precision
	Source    FixSource
	Provider  string
	FixedAt   time.Time
}

func (f Fix) Validate() error {
	if err := f.Point.Validate(); err != nil {
		return err
	}
	if !f.Precision.Known() {
		return fmt.Errorf("%w: unknown precision %q", ErrInvalidFix, f.Precision)
	}
	if !knownSources[f.Source] {
		return fmt.Errorf("%w: unknown source %q", ErrInvalidFix, f.Source)
	}
	if f.Source == SourceProvider && strings.TrimSpace(f.Provider) == "" {
		return fmt.Errorf("%w: a provider fix names its provider", ErrInvalidFix)
	}
	return nil
}

func CEPPrecision(zip string, spreadM float64) Precision {
	if _, err := cep.Parse(zip); err != nil || cep.IsGeneric(zip) {
		return PrecisionCity
	}
	switch {
	case !finite(spreadM) || spreadM < 0:
		return PrecisionCity
	case spreadM < streetSpreadM:
		return PrecisionStreet
	case spreadM <= postalCodeSpreadM:
		return PrecisionPostalCode
	}
	return PrecisionCity
}

func Choose(current *Fix, candidates []Fix) (Fix, bool) {
	var best *Fix
	for i := range candidates {
		candidate := candidates[i]
		if candidate.Validate() != nil {
			continue
		}
		if best == nil || candidate.Precision.Better(best.Precision) {
			best = &candidates[i]
		}
	}
	if current == nil {
		if best == nil {
			return Fix{}, false
		}
		return *best, true
	}
	if current.Source.Confirmed() || best == nil || !best.Precision.Better(current.Precision) {
		return *current, false
	}
	return *best, true
}

func SameFix(a, b *Fix) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
