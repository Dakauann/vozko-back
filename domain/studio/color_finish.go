package studio

import (
	"strings"

	"vozko/domain/mediagen"
)

type CurvePoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type ColorCurves struct {
	Master []CurvePoint `json:"master,omitempty"`
	Red    []CurvePoint `json:"red,omitempty"`
	Green  []CurvePoint `json:"green,omitempty"`
	Blue   []CurvePoint `json:"blue,omitempty"`
}

type HueSecondary struct {
	Hue        float64  `json:"hue"`
	Width      float64  `json:"width"`
	Softness   *float64 `json:"softness,omitempty"`
	Desaturate *float64 `json:"desaturate,omitempty"`
	Shift      *float64 `json:"shift,omitempty"`
	Saturation *float64 `json:"saturation,omitempty"`
}

type GradeLut struct {
	AssetID   string   `json:"assetId"`
	Intensity *float64 `json:"intensity,omitempty"`
}

const (
	MaxHueDegrees      = 360.0
	MaxHueSoftness     = 180.0
	MaxHueShift        = 180.0
	MaxHueDesaturation = 100.0
)

func curvePointsIssue(points []CurvePoint) string {
	if points == nil {
		return ""
	}
	if len(points) < 2 {
		return CodeInvalid
	}
	for i, p := range points {
		if !within(p.X, 0, 1) || !within(p.Y, 0, 1) {
			return CodeOutOfRange
		}
		if i > 0 && p.X <= points[i-1].X {
			return CodeInvalid
		}
	}
	return ""
}

func (c *ColorCurves) issue() string {
	if c == nil {
		return ""
	}
	for _, points := range [][]CurvePoint{c.Master, c.Red, c.Green, c.Blue} {
		if code := curvePointsIssue(points); code != "" {
			return code
		}
	}
	return ""
}

func optionalWithin(v *float64, lo, hi float64) bool {
	return v == nil || within(*v, lo, hi)
}

func (h *HueSecondary) issue() string {
	switch {
	case h == nil:
		return ""
	case !within(h.Hue, 0, MaxHueDegrees) || !within(h.Width, 0, MaxHueDegrees):
		return CodeOutOfRange
	case !optionalWithin(h.Softness, 0, MaxHueSoftness) || !optionalWithin(h.Desaturate, 0, MaxHueDesaturation):
		return CodeOutOfRange
	case !optionalWithin(h.Shift, -MaxHueShift, MaxHueShift) || !optionalWithin(h.Saturation, -MaxGradeSlider, MaxGradeSlider):
		return CodeOutOfRange
	}
	return ""
}

func (l *GradeLut) issue() string {
	switch {
	case l == nil:
		return ""
	case strings.TrimSpace(l.AssetID) == "":
		return CodeRequired
	case !optionalWithin(l.Intensity, 0, 1):
		return CodeOutOfRange
	}
	return ""
}

type GradeKeyframes struct {
	Exposure    []mediagen.Keyframe `json:"exposure,omitempty"`
	Contrast    []mediagen.Keyframe `json:"contrast,omitempty"`
	Highlights  []mediagen.Keyframe `json:"highlights,omitempty"`
	Shadows     []mediagen.Keyframe `json:"shadows,omitempty"`
	Whites      []mediagen.Keyframe `json:"whites,omitempty"`
	Blacks      []mediagen.Keyframe `json:"blacks,omitempty"`
	Temperature []mediagen.Keyframe `json:"temperature,omitempty"`
	Tint        []mediagen.Keyframe `json:"tint,omitempty"`
	Saturation  []mediagen.Keyframe `json:"saturation,omitempty"`
	Vibrance    []mediagen.Keyframe `json:"vibrance,omitempty"`
}

func (k *GradeKeyframes) sliders() [][]mediagen.Keyframe {
	return [][]mediagen.Keyframe{k.Contrast, k.Highlights, k.Shadows, k.Whites, k.Blacks, k.Temperature, k.Tint, k.Saturation, k.Vibrance}
}

func (k *GradeKeyframes) count() int {
	total := len(k.Exposure)
	for _, frames := range k.sliders() {
		total += len(frames)
	}
	return total
}

func (k *GradeKeyframes) issue() string {
	if code := keysIssue(k.Exposure, -MaxExposureStops, MaxExposureStops); code != "" {
		return code
	}
	for _, frames := range k.sliders() {
		if code := keysIssue(frames, -MaxGradeSlider, MaxGradeSlider); code != "" {
			return code
		}
	}
	return ""
}

func (c Clip) colorFinishIssue() string {
	media := c.Type == ClipVideo || c.Type == ClipImage
	keyed := c.Keyframes != nil && c.Keyframes.GradeKeyframes.count() > 0
	if (c.GradientMap != nil && !media) || (keyed && !media && c.Type != ClipGroup) {
		return CodeInvalid
	}
	if c.GradientMap != nil {
		return c.GradientMap.issue()
	}
	return ""
}
