package studio

func point(fallback float64) vfxParam { return keyed(-0.5, 1.5, fallback) }

func soundLayout(y float64) map[string]vfxParam {
	return map[string]vfxParam{
		"amount": keyed(0, 1, 1), "x": point(0.5), "y": point(y), "width": keyed(0.05, 1, 0.8), "size": keyed(0, 1, 0.3),
		"gap": keyed(0, 0.9, 0.3), "roundness": keyed(0, 1, 1), "gain": keyed(-24, 24, 0),
	}
}

func withParams(base map[string]vfxParam, extra map[string]vfxParam) map[string]vfxParam {
	out := make(map[string]vfxParam, len(base)+len(extra))
	for name, spec := range base {
		out[name] = spec
	}
	for name, spec := range extra {
		out[name] = spec
	}
	return out
}

var packVfxKinds = map[string]vfxKind{
	"glow":            {params: map[string]vfxParam{"amount": keyed(0, 1, 0.6), "threshold": keyed(0, 1, 0.6), "size": keyed(1, 200, 24)}, colors: map[string]string{"color": "#ffffff"}},
	"mosaic":          {params: map[string]vfxParam{"amount": keyed(0, 1, 1), "size": keyed(2, 200, 24)}},
	"posterize":       {params: map[string]vfxParam{"amount": keyed(0, 1, 1), "levels": keyed(2, 32, 5)}},
	"mirror":          {params: map[string]vfxParam{"amount": keyed(0, 1, 1), "x": keyed(0, 1, 0.5), "y": keyed(0, 1, 0.5), "angle": keyed(-180, 180, 0)}},
	"offset":          {params: map[string]vfxParam{"amount": keyed(0, 1, 1), "shiftX": keyed(-1, 1, 0.25), "shiftY": keyed(-1, 1, 0)}},
	"directionalBlur": {params: map[string]vfxParam{"amount": keyed(0, 1, 1), "length": keyed(0, 400, 30), "angle": keyed(-180, 180, 0)}},
	"zoomBlur":        {params: map[string]vfxParam{"amount": keyed(0, 1, 1), "length": keyed(0, 1, 0.2), "x": point(0.5), "y": point(0.5)}},
	"spinBlur":        {params: map[string]vfxParam{"amount": keyed(0, 1, 1), "angle": keyed(0, 180, 12), "x": point(0.5), "y": point(0.5)}},
	"waveWarp": {params: map[string]vfxParam{
		"amount": keyed(0, 1, 1), "height": keyed(0, 200, 12), "length": keyed(4, 2000, 160), "angle": keyed(-180, 180, 0), "speed": paced(-10, 10, 1), "phase": keyed(0, 360, 0),
	}},
	"ripple": {params: map[string]vfxParam{
		"amount": keyed(0, 1, 1), "x": point(0.5), "y": point(0.5), "height": keyed(0, 200, 10), "length": keyed(4, 2000, 80), "size": keyed(0, 2, 0.8), "speed": paced(-10, 10, 1), "phase": keyed(0, 360, 0),
	}},
	"twirl": {params: map[string]vfxParam{"amount": keyed(0, 1, 1), "angle": keyed(-1080, 1080, 180), "size": keyed(0, 2, 0.5), "x": point(0.5), "y": point(0.5)}},
	"bulge": {params: map[string]vfxParam{"amount": keyed(0, 1, 1), "height": keyed(-1, 1, 0.5), "size": keyed(0, 2, 0.4), "x": point(0.5), "y": point(0.5)}},
	"cornerPin": {params: map[string]vfxParam{
		"amount": keyed(0, 1, 1), "topLeftX": point(0), "topLeftY": point(0), "topRightX": point(1), "topRightY": point(0),
		"bottomRightX": point(1), "bottomRightY": point(1), "bottomLeftX": point(0), "bottomLeftY": point(1),
	}},
	"fractalNoise": {
		params: map[string]vfxParam{"amount": keyed(0, 1, 1), "size": keyed(4, 2000, 160), "complexity": keyed(1, 8, 4), "contrast": keyed(0, 4, 1), "brightness": keyed(-1, 1, 0), "speed": paced(0, 4, 0.5)},
		colors: map[string]string{"color": "#000000", "color2": "#ffffff"},
	},
	"gradientRamp": {
		params: map[string]vfxParam{"amount": keyed(0, 1, 1), "x": point(0.5), "y": point(0), "toX": point(0.5), "toY": point(1), "radial": keyed(0, 1, 0)},
		colors: map[string]string{"color": "#000000", "color2": "#ffffff"},
	},
	"fourColorGradient": {
		params: map[string]vfxParam{
			"amount": keyed(0, 1, 1), "topLeftX": point(0.15), "topLeftY": point(0.15), "topRightX": point(0.85), "topRightY": point(0.15),
			"bottomRightX": point(0.85), "bottomRightY": point(0.85), "bottomLeftX": point(0.15), "bottomLeftY": point(0.85), "softness": keyed(0.01, 1, 0.5),
		},
		colors: map[string]string{"color": "#ff4d6d", "color2": "#ffb347", "color3": "#4dd2ff", "color4": "#7a5cff"},
	},
	"audioSpectrum": {params: withParams(soundLayout(0.85), map[string]vfxParam{"count": paced(4, 128, 32)}), colors: map[string]string{"color": "#ffffff", "color2": "#7ad7ff"}},
	"audioWaveform": {params: withParams(soundLayout(0.5), map[string]vfxParam{"count": paced(8, 512, 64), "window": paced(250, 10_000, 3000)}), colors: map[string]string{"color": "#ffffff", "color2": "#7ad7ff"}},
	"posterizeTime": {params: map[string]vfxParam{"rate": paced(1, 30, 8)}},
	"echo":          {params: map[string]vfxParam{"amount": keyed(0, 1, 1), "count": paced(1, 32, 6), "delay": paced(10, 1000, 80), "decay": keyed(0, 1, 0.6)}},
}

func mergeVfxKinds(parts ...map[string]vfxKind) map[string]vfxKind {
	out := map[string]vfxKind{}
	for _, part := range parts {
		for name, kind := range part {
			out[name] = kind
		}
	}
	return out
}

var soundVfxKinds = map[string]bool{"audioSpectrum": true, "audioWaveform": true}

var clipOnlyVfxKinds = map[string]bool{"echo": true}

func SoundVfxKinds() []string {
	return []string{"audioSpectrum", "audioWaveform"}
}

func (v Vfx) trackIssue() string {
	if soundVfxKinds[v.Kind] {
		if !validToken(v.TrackID) {
			return CodeInvalid
		}
		return ""
	}
	if v.TrackID != "" {
		return CodeInvalid
	}
	return ""
}

func effectVfxIssue(list []Vfx) string {
	for _, v := range list {
		if clipOnlyVfxKinds[v.Kind] {
			return CodeInvalid
		}
	}
	return ""
}

func (d VideoDocument) vfxTracksIssue() string {
	audio := map[string]bool{}
	for _, track := range d.Tracks {
		if track.Kind == TrackAudio {
			audio[track.ID] = true
		}
	}
	for _, track := range d.Tracks {
		for _, c := range track.Clips {
			for _, v := range c.Vfx {
				if soundVfxKinds[v.Kind] && !audio[v.TrackID] {
					return CodeInvalid
				}
			}
		}
	}
	return ""
}

func VfxParamIssue(kind, name string, value float64) string {
	spec, ok := vfxKinds[kind].params[name]
	if !ok {
		return CodeInvalid
	}
	if !within(value, spec.low, spec.high) {
		return CodeOutOfRange
	}
	return ""
}

func VfxParamRange(kind, name string) (float64, float64, bool) {
	spec, ok := vfxKinds[kind].params[name]
	return spec.low, spec.high, ok
}

func VfxColorNames() []string {
	return []string{"color", "color2", "color3", "color4"}
}

func VfxColorAllowed(kind, name, value string) bool {
	_, ok := vfxKinds[kind].colors[name]
	return ok && hexColor(value)
}
