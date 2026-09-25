package opportunity

import (
	"math"
	"strconv"
	"strings"
	"time"
)

type StageRef struct {
	ID         string
	PipelineID string
	IsWon      bool
	IsLost     bool
}

func (s StageRef) status() Status {
	switch {
	case s.IsWon:
		return StatusWon
	case s.IsLost:
		return StatusLost
	}
	return StatusOpen
}

func (o *Opportunity) PlaceOn(stage StageRef, actorID string, at time.Time) error {
	if stage.PipelineID != o.PipelineID {
		return ErrStageOutsidePipeline
	}
	previous := o.Status
	o.StageID = stage.ID
	o.Status = stage.status()

	switch {
	case !o.IsClosed():
		o.CloseDate = nil
		o.ClosedBy = ""
	case o.Status != previous || o.CloseDate == nil:
		closedAt := at
		o.CloseDate = &closedAt
		o.ClosedBy = actorID
	}
	return nil
}

const maxAmountCents = 1e15

func CentsFromAmount(amount float64) (int64, error) {
	if math.IsNaN(amount) || math.IsInf(amount, 0) || amount < 0 {
		return 0, ErrInvalidAmount
	}
	cents := math.Round(amount * 100)
	if cents > maxAmountCents {
		return 0, ErrInvalidAmount
	}
	return int64(cents), nil
}

func CentsFromText(text string) (int64, error) {
	amount, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil {
		return 0, ErrInvalidAmount
	}
	return CentsFromAmount(amount)
}
