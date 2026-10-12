package studio

import (
	"math"

	"vozko/domain/mediagen"
)

const (
	MinBehaviorFrequency = 0.1
	MaxBehaviorFrequency = 30.0
	MaxBehaviorAmplitude = 1.0
	MinBehaviorDecay     = 0.1
	MaxBehaviorDecay     = 30.0
	MinBehaviorDB        = -70.0
	MaxBehaviorDB        = 0.0
	MaxBehaviorSeed      = 999_999
)

type Behavior struct {
	ID        string   `json:"id"`
	Kind      string   `json:"kind"`
	Property  string   `json:"property"`
	OffsetMS  *int64   `json:"offsetMs,omitempty"`
	Frequency *float64 `json:"frequency,omitempty"`
	Amount    *float64 `json:"amount,omitempty"`
	Seed      *int64   `json:"seed,omitempty"`
	Mode      string   `json:"mode,omitempty"`
	Side      string   `json:"side,omitempty"`
	Rate      *float64 `json:"rate,omitempty"`
	Amplitude *float64 `json:"amplitude,omitempty"`
	Decay     *float64 `json:"decay,omitempty"`
	TrackID   string   `json:"trackId,omitempty"`
	FloorDB   *float64 `json:"floorDb,omitempty"`
	CeilingDB *float64 `json:"ceilingDb,omitempty"`
}

var behaviorFields = map[string][]string{
	"wiggle":     {"frequency", "amount", "seed"},
	"loop":       {"mode", "side"},
	"drift":      {"rate"},
	"overshoot":  {"amplitude", "frequency", "decay"},
	"audioReact": {"trackId", "amount", "floorDb", "ceilingDb"},
}

var behaviorSpans = map[string]float64{
	"x":        mediagen.MaxKeyframePosition - mediagen.MinKeyframePosition,
	"y":        mediagen.MaxKeyframePosition - mediagen.MinKeyframePosition,
	"scale":    mediagen.MaxKeyframeScale - mediagen.MinKeyframeScale,
	"scaleX":   mediagen.MaxKeyframeScale - mediagen.MinKeyframeScale,
	"scaleY":   mediagen.MaxKeyframeScale - mediagen.MinKeyframeScale,
	"rotation": 2 * mediagen.MaxKeyframeRotation,
	"opacity":  1,
	"blur":     MaxClipBlur,
}

var loopModes = map[string]bool{"cycle": true, "pingpong": true, "offset": true, "continue": true}

var loopSides = map[string]bool{"out": true, "in": true, "both": true}

func BehaviorKinds() []string {
	return []string{"wiggle", "loop", "drift", "overshoot", "audioReact"}
}

func BehaviorProperties() []string {
	return []string{"x", "y", "scale", "scaleX", "scaleY", "rotation", "opacity", "blur"}
}

func behaviorPropertiesOf(t ClipType) map[string]bool {
	switch t {
	case ClipVideo, ClipImage, ClipOverlay:
		allowed := map[string]bool{}
		for _, property := range BehaviorProperties() {
			allowed[property] = true
		}
		return allowed
	case ClipNull:
		allowed := map[string]bool{}
		for _, property := range BehaviorProperties() {
			if nullLanes[property] {
				allowed[property] = true
			}
		}
		return allowed
	}
	return nil
}

func (b Behavior) carries() map[string]bool {
	return map[string]bool{
		"frequency": b.Frequency != nil, "amount": b.Amount != nil, "seed": b.Seed != nil, "mode": b.Mode != "", "side": b.Side != "",
		"rate": b.Rate != nil, "amplitude": b.Amplitude != nil, "decay": b.Decay != nil, "trackId": b.TrackID != "",
		"floorDb": b.FloorDB != nil, "ceilingDb": b.CeilingDB != nil,
	}
}

func (b Behavior) fieldsMatch() bool {
	wanted := map[string]bool{}
	for _, field := range behaviorFields[b.Kind] {
		wanted[field] = true
	}
	for field, present := range b.carries() {
		if present != wanted[field] {
			return false
		}
	}
	return true
}

func behaviorNumber(v *float64) float64 {
	if v == nil {
		return math.NaN()
	}
	return *v
}

func (b Behavior) paramsIssue() string {
	span := behaviorSpans[b.Property]
	switch b.Kind {
	case "wiggle":
		if !within(behaviorNumber(b.Frequency), MinBehaviorFrequency, MaxBehaviorFrequency) || !within(behaviorNumber(b.Amount), 0, span) || *b.Seed < 0 || *b.Seed > MaxBehaviorSeed {
			return CodeOutOfRange
		}
	case "loop":
		if !loopModes[b.Mode] || !loopSides[b.Side] {
			return CodeUnknown
		}
	case "drift":
		if !within(behaviorNumber(b.Rate), -span, span) {
			return CodeOutOfRange
		}
	case "overshoot":
		if !within(behaviorNumber(b.Amplitude), 0, MaxBehaviorAmplitude) || !within(behaviorNumber(b.Frequency), MinBehaviorFrequency, MaxBehaviorFrequency) || !within(behaviorNumber(b.Decay), MinBehaviorDecay, MaxBehaviorDecay) {
			return CodeOutOfRange
		}
	case "audioReact":
		if !validToken(b.TrackID) {
			return CodeInvalid
		}
		floor, ceiling := behaviorNumber(b.FloorDB), behaviorNumber(b.CeilingDB)
		if !within(behaviorNumber(b.Amount), -span, span) || !within(floor, MinBehaviorDB, MaxBehaviorDB) || !within(ceiling, MinBehaviorDB, MaxBehaviorDB) || floor >= ceiling {
			return CodeOutOfRange
		}
	}
	return ""
}

func (b Behavior) issue(allowed map[string]bool) string {
	if !validToken(b.ID) {
		return CodeInvalid
	}
	if _, known := behaviorFields[b.Kind]; !known {
		return CodeUnknown
	}
	if _, known := behaviorSpans[b.Property]; !known {
		return CodeUnknown
	}
	if !allowed[b.Property] || !b.fieldsMatch() {
		return CodeInvalid
	}
	if b.OffsetMS != nil && (*b.OffsetMS < 0 || *b.OffsetMS > MaxVideoMS) {
		return CodeOutOfRange
	}
	return b.paramsIssue()
}

func (c Clip) behaviorsIssue() string {
	if len(c.Behaviors) == 0 {
		return ""
	}
	allowed := behaviorPropertiesOf(c.Type)
	if len(allowed) == 0 {
		return CodeInvalid
	}
	ids := map[string]bool{}
	pairs := map[string]bool{}
	for _, b := range c.Behaviors {
		if code := b.issue(allowed); code != "" {
			return code
		}
		pair := b.Property + ":" + b.Kind
		if ids[b.ID] || pairs[pair] {
			return CodeDuplicate
		}
		ids[b.ID] = true
		pairs[pair] = true
	}
	return ""
}

func (d VideoDocument) behaviorTracksIssue() string {
	audio := map[string]bool{}
	for _, track := range d.Tracks {
		if track.Kind == TrackAudio {
			audio[track.ID] = true
		}
	}
	for _, track := range d.Tracks {
		for _, c := range track.Clips {
			for _, b := range c.Behaviors {
				if b.Kind == "audioReact" && !audio[b.TrackID] {
					return CodeInvalid
				}
			}
		}
	}
	return ""
}
