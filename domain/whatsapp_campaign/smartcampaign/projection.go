package smartcampaign

import (
	"fmt"
	"time"
)

const maxProjectedWaves = 60

type PlannedWave struct {
	Number int
	Size   int
	At     time.Time
}

type Projection struct {
	Waves     []PlannedWave
	Truncated bool
}

func Project(p Policy, contacts int, start time.Time) (Projection, error) {
	hours, err := p.SendHours.Compile()
	if err != nil || hours == nil {
		return Projection{}, fmt.Errorf("%w: send hours do not compile", ErrInvalidPolicy)
	}
	var plan Projection
	var history History
	var day time.Time
	today := 0
	next := start
	for left := contacts; left > 0; {
		if len(plan.Waves) == maxProjectedWaves {
			plan.Truncated = true
			break
		}
		opens, ok := hours.NextOpen(next)
		if !ok {
			return Projection{}, fmt.Errorf("%w: send hours never open", ErrInvalidPolicy)
		}
		if !p.DayStart(opens).Equal(day) {
			day, today = p.DayStart(opens), 0
		}
		if p.MaxWavesPerDay > 0 && today >= p.MaxWavesPerDay {
			opens = nextDayOpening(hours, opens)
			day, today = p.DayStart(opens), 0
		}
		history.CurrentSize = p.nextSize(history, false)
		size := min(history.CurrentSize, left)
		plan.Waves = append(plan.Waves, PlannedWave{Number: len(plan.Waves) + 1, Size: size, At: opens})
		left -= size
		today++
		next = opens.Add(p.ObserveWait)
	}
	return plan, nil
}
