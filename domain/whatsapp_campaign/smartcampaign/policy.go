package smartcampaign

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"vozko/domain/working_hours"
)

type Preset string

const (
	Conservative Preset = "conservative"
	Standard     Preset = "standard"
	Aggressive   Preset = "aggressive"
)

var Presets = []Preset{Conservative, Standard, Aggressive}

var ErrInvalidPolicy = errors.New("smart campaign: invalid wave policy")

type Policy struct {
	Preset              Preset              `json:"preset"`
	FirstWaveSize       int                 `json:"firstWaveSize"`
	MinWaveSize         int                 `json:"minWaveSize"`
	MaxWaveSize         int                 `json:"maxWaveSize"`
	Growth              float64             `json:"growth"`
	Shrink              float64             `json:"shrink"`
	ObserveWait         time.Duration       `json:"observeWait" swaggertype:"integer"`
	MaxObserveWait      time.Duration       `json:"maxObserveWait" swaggertype:"integer"`
	MaxWavesPerDay      int                 `json:"maxWavesPerDay"`
	MaxSlowWavesAtFloor int                 `json:"maxSlowWavesAtFloor"`
	MinSample           int                 `json:"minSample"`
	FailureRateSlow     float64             `json:"failureRateSlow"`
	FailureRateStop     float64             `json:"failureRateStop"`
	EngagementRateSlow  float64             `json:"engagementRateSlow"`
	OnTemplateYellow    Action              `json:"onTemplateYellow"`
	OnPhoneYellow       Action              `json:"onPhoneYellow"`
	OnPhoneRed          Action              `json:"onPhoneRed"`
	SendHours           *working_hours.Spec `json:"sendHours"`
}

func ParsePreset(raw string) (Preset, error) {
	for _, preset := range Presets {
		if string(preset) == strings.TrimSpace(raw) {
			return preset, nil
		}
	}
	return "", fmt.Errorf("%w: unknown preset %q", ErrInvalidPolicy, raw)
}

func PresetPolicy(preset Preset, sendHours *working_hours.Spec) Policy {
	standard := Policy{
		Preset:              Standard,
		FirstWaveSize:       200,
		MinWaveSize:         50,
		MaxWaveSize:         5000,
		Growth:              2,
		Shrink:              0.5,
		ObserveWait:         2 * time.Hour,
		MaxObserveWait:      5 * time.Hour,
		MaxSlowWavesAtFloor: 3,
		MinSample:           50,
		FailureRateSlow:     0.08,
		FailureRateStop:     0.15,
		EngagementRateSlow:  0.03,
		OnTemplateYellow:    Pause,
		OnPhoneYellow:       SlowDown,
		OnPhoneRed:          Pause,
		SendHours:           sendHours,
	}
	switch preset {
	case Conservative:
		p := standard
		p.Preset = Conservative
		p.FirstWaveSize, p.MaxWaveSize, p.Growth = 100, 2000, 1.5
		p.ObserveWait = 3 * time.Hour
		p.FailureRateSlow, p.FailureRateStop, p.EngagementRateSlow = 0.05, 0.10, 0.02
		p.OnPhoneYellow, p.OnPhoneRed = Pause, Stop
		return p
	case Aggressive:
		p := standard
		p.Preset = Aggressive
		p.FirstWaveSize, p.MinWaveSize, p.MaxWaveSize, p.Growth = 500, 100, 20000, 3
		p.ObserveWait = time.Hour
		p.FailureRateSlow, p.FailureRateStop, p.EngagementRateSlow = 0.10, 0.20, 0.05
		p.OnTemplateYellow = SlowDown
		return p
	default:
		return standard
	}
}

func (p Policy) WithPhoneDefaults() Policy {
	preset := PresetPolicy(p.Preset, p.SendHours)
	if p.OnPhoneYellow == "" {
		p.OnPhoneYellow = preset.OnPhoneYellow
	}
	if p.OnPhoneRed == "" {
		p.OnPhoneRed = preset.OnPhoneRed
	}
	return p
}

func (p Policy) Validate() error {
	var problems []string
	check := func(ok bool, problem string) {
		if !ok {
			problems = append(problems, problem)
		}
	}
	_, presetErr := ParsePreset(string(p.Preset))
	check(presetErr == nil, "unknown preset")
	check(p.MinWaveSize >= 1, "min wave size must be at least 1")
	check(p.MinWaveSize <= p.FirstWaveSize && p.FirstWaveSize <= p.MaxWaveSize, "wave sizes must satisfy min <= first <= max")
	check(p.Growth >= 1 && p.Growth <= 4, "growth must be between 1 and 4")
	check(p.Shrink > 0 && p.Shrink < 1, "shrink must be between 0 and 1")
	check(p.ObserveWait >= 20*time.Minute, "observe wait must be at least 20 minutes")
	check(p.MaxObserveWait >= p.ObserveWait && p.MaxObserveWait <= 24*time.Hour, "max observe wait must be between observe wait and 24 hours")
	check(p.MaxWavesPerDay >= 0, "max waves per day cannot be negative")
	check(p.MaxSlowWavesAtFloor >= 1 && p.MaxSlowWavesAtFloor <= 10, "slowed waves at the floor must be between 1 and 10")
	check(p.MinSample >= 20, "min sample must be at least 20 contacts")
	check(p.FailureRateSlow > 0 && p.FailureRateSlow < p.FailureRateStop && p.FailureRateStop <= 0.5, "failure rates must satisfy 0 < slow < stop <= 50%")
	check(p.EngagementRateSlow > 0 && p.EngagementRateSlow < 1, "engagement rate must be between 0 and 100%")
	check(p.OnTemplateYellow.protects(), "a yellow template must at least slow down")
	check(p.OnPhoneYellow.protects(), "a yellow phone must at least slow down")
	check(p.OnPhoneRed.protects(), "a red phone must at least slow down")
	check(p.SendHours != nil, "send hours are required")
	if p.SendHours != nil {
		if err := p.SendHours.Validate(); err != nil {
			problems = append(problems, err.Error())
		}
	}
	if len(problems) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrInvalidPolicy, strings.Join(problems, "; "))
}

func (p Policy) nextSize(h History, slow bool) int {
	switch {
	case h.ResumedAfterHold:
		return p.MinWaveSize
	case h.CurrentSize == 0:
		return p.FirstWaveSize
	case h.Interrupted:
		return h.CurrentSize
	case slow:
		return max(p.MinWaveSize, scale(h.CurrentSize, p.Shrink))
	default:
		return min(p.MaxWaveSize, scale(h.CurrentSize, p.Growth))
	}
}

func (p Policy) stuckAtFloor(h History) bool {
	return h.CurrentSize <= p.MinWaveSize && h.SlowStreakAtFloor+1 >= p.MaxSlowWavesAtFloor
}

func scale(size int, factor float64) int {
	return int(math.Round(float64(size) * factor))
}

func (p Policy) DayStart(now time.Time) time.Time {
	loc := time.UTC
	if hours, err := p.SendHours.Compile(); err == nil && hours != nil {
		loc = hours.Location()
	}
	local := now.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
}
