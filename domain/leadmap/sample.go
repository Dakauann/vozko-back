package leadmap

import (
	"context"
	"errors"

	"vozko/domain/crmfilter"
	"vozko/domain/geo"
	"vozko/domain/lead"
)

const MaxSamplePoints = 1000

var ErrSampleLimit = errors.New("leadmap: a sample holds 1 to 1000 positions")

type Sample struct {
	Points    []Point
	Located   int
	Positions int
	Bounds    *geo.BBox
}

func (s Sample) Sampled() bool {
	return s.Positions > len(s.Points)
}

func CheckSampleLimit(limit int) error {
	if limit < 1 || limit > MaxSamplePoints {
		return ErrSampleLimit
	}
	return nil
}

func AreaBox(areas []crmfilter.AreaBounds) *geo.BBox {
	v, ok := AreaViewport(areas, Summary{})
	if !ok {
		return nil
	}
	return &v.BBox
}

type SampleReader interface {
	SelectionSample(ctx context.Context, q lead.SelectionQuery, limit int) (Sample, error)
}
