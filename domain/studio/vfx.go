package studio

import (
	"encoding/json"
	"math"

	"vozko/domain/mediagen"
)

const (
	ClipEffect ClipType = "effect"

	MaxVfxSeed = 999_999
)

type Vfx struct {
	ID        string                         `json:"id"`
	Kind      string                         `json:"kind"`
	Seed      int64                          `json:"seed"`
	Hidden    bool                           `json:"hidden,omitempty"`
	OffsetMS  int64                          `json:"offsetMs,omitempty"`
	Params    map[string]*float64            `json:"params,omitempty"`
	Colors    map[string]*string             `json:"colors,omitempty"`
	Keyframes map[string][]mediagen.Keyframe `json:"keyframes,omitempty"`
	TrackID   string                         `json:"trackId,omitempty"`
}

type vfxParam struct {
	low, high, fallback float64
	keyed               bool
}

type vfxKind struct {
	params map[string]vfxParam
	colors map[string]string
}

func keyed(low, high, fallback float64) vfxParam { return vfxParam{low, high, fallback, true} }
func paced(low, high, fallback float64) vfxParam { return vfxParam{low, high, fallback, false} }

var vfxKinds = mergeVfxKinds(baseVfxKinds, packVfxKinds)

var baseVfxKinds = map[string]vfxKind{
	"grain": {params: map[string]vfxParam{"amount": keyed(0, 1, 0.35), "size": keyed(0.5, 8, 1.5), "rate": paced(1, 60, 24), "chroma": keyed(0, 1, 0)}},
	"lightLeak": {
		params: map[string]vfxParam{"amount": keyed(0, 1, 0.7), "size": keyed(0.2, 2, 0.9), "speed": paced(0, 4, 1), "x": keyed(-0.5, 1.5, 0.15), "y": keyed(-0.5, 1.5, 0.2)},
		colors: map[string]string{"color": "#ff7a2f", "color2": "#ff2f6d"},
	},
	"glitch": {params: map[string]vfxParam{"amount": keyed(0, 1, 0.5), "frequency": paced(0, 30, 3), "split": keyed(0, 100, 12), "size": keyed(0.005, 0.5, 0.06), "scanlines": keyed(0, 1, 0.2)}},
	"particles": {
		params: map[string]vfxParam{"amount": keyed(0, 1, 0.85), "count": paced(1, 1000, 80), "size": keyed(0.5, 80, 6), "speed": paced(0, 4, 1), "angle": paced(-180, 180, 90), "softness": keyed(0, 1, 0.5), "twinkle": keyed(0, 1, 0.3)},
		colors: map[string]string{"color": "#ffffff"},
	},
	"lensFlare": {
		params: map[string]vfxParam{"amount": keyed(0, 1, 0.8), "x": keyed(-0.5, 1.5, 0.25), "y": keyed(-0.5, 1.5, 0.25), "size": keyed(0.2, 3, 1), "ghosts": keyed(0, 1, 0.6), "streak": keyed(0, 1, 0.4), "flicker": keyed(0, 1, 0.15)},
		colors: map[string]string{"color": "#ffd29a"},
	},
	"shake": {params: map[string]vfxParam{"amount": keyed(0, 1, 0.3), "frequency": paced(0.2, 30, 6), "rotation": keyed(0, 15, 1)}},
	"vignette": {
		params: map[string]vfxParam{"amount": keyed(0, 1, 0.5), "size": keyed(0, 1, 0.5), "softness": keyed(0.01, 1, 0.5), "roundness": keyed(0, 1, 1)},
		colors: map[string]string{"color": "#000000"},
	},
	"chromaticAberration": {params: map[string]vfxParam{"amount": keyed(0, 1, 0.4), "radial": keyed(0, 1, 1), "angle": keyed(-180, 180, 0)}},
}

func VfxKinds() []string {
	out := make([]string, 0, len(vfxKinds))
	for kind := range vfxKinds {
		out = append(out, kind)
	}
	return out
}

func VfxParams() []string {
	seen := map[string]bool{}
	out := []string{}
	for _, kind := range vfxKinds {
		for name := range kind.params {
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	return out
}

func hexColor(c string) bool {
	if len(c) != 7 || c[0] != '#' {
		return false
	}
	for _, r := range c[1:] {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}

func (v Vfx) issue() string {
	kind, known := vfxKinds[v.Kind]
	if !known {
		return CodeUnknown
	}
	if v.Seed < 0 || v.Seed > MaxVfxSeed || v.OffsetMS < 0 || v.OffsetMS > MaxVideoMS {
		return CodeOutOfRange
	}
	if code := v.trackIssue(); code != "" {
		return code
	}
	for name, value := range v.Params {
		if value == nil {
			continue
		}
		spec, ok := kind.params[name]
		if !ok {
			return CodeInvalid
		}
		if math.IsNaN(*value) || !within(*value, spec.low, spec.high) {
			return CodeOutOfRange
		}
	}
	for name, value := range v.Colors {
		if value == nil {
			continue
		}
		if _, ok := kind.colors[name]; !ok || !hexColor(*value) {
			return CodeInvalid
		}
	}
	for name, frames := range v.Keyframes {
		if len(frames) == 0 {
			continue
		}
		spec, ok := kind.params[name]
		if !ok || !spec.keyed {
			return CodeInvalid
		}
		if code := keysIssue(frames, spec.low, spec.high); code != "" {
			return code
		}
	}
	return ""
}

func vfxIssue(list []Vfx) string {
	seen := make(map[string]bool, len(list))
	for _, v := range list {
		if !validToken(v.ID) || seen[v.ID] {
			return CodeInvalid
		}
		seen[v.ID] = true
		if code := v.issue(); code != "" {
			return code
		}
	}
	return ""
}

var effectClipFields = withClipTags(map[string]bool{
	"id": true, "type": true, "startMs": true, "durationMs": true, "trimInMs": true, "volume": true,
	"fadeInMs": true, "fadeOutMs": true, "transform": true, "vfx": true, "disabled": true,
	"color": true, "gradientMap": true, "blur": true, "keyframes": true, "masks": true, "camera": true, "motionBlur": true,
}, true)

func carriesOnly(value any, allowed map[string]bool) bool {
	raw, err := json.Marshal(value)
	if err != nil {
		return false
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return false
	}
	for name := range fields {
		if !allowed[name] {
			return false
		}
	}
	return true
}

func (c Clip) effectIssue() string {
	if !carriesOnly(c, effectClipFields) {
		return CodeInvalid
	}
	t := c.Transform
	if c.TrimInMS != 0 || t.X != 0.5 || t.Y != 0.5 || t.W != 1 || t.H != 1 || t.Rotation != 0 {
		return CodeInvalid
	}
	if !within(t.Opacity, 0, 1) {
		return CodeOutOfRange
	}
	return c.adjustmentIssue()
}
