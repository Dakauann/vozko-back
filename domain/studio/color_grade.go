package studio

type ColorGrade struct {
	Exposure    float64 `json:"exposure,omitempty"`
	Contrast    float64 `json:"contrast,omitempty"`
	Highlights  float64 `json:"highlights,omitempty"`
	Shadows     float64 `json:"shadows,omitempty"`
	Whites      float64 `json:"whites,omitempty"`
	Blacks      float64 `json:"blacks,omitempty"`
	Temperature float64 `json:"temperature,omitempty"`
	Tint        float64 `json:"tint,omitempty"`
	Saturation  float64 `json:"saturation,omitempty"`
	Vibrance    float64 `json:"vibrance,omitempty"`
}

const (
	MaxExposureStops = 5.0
	MaxGradeSlider   = 100.0
)

func (g *ColorGrade) issue() string {
	if g == nil {
		return ""
	}
	if !within(g.Exposure, -MaxExposureStops, MaxExposureStops) {
		return CodeOutOfRange
	}
	for _, v := range []float64{g.Contrast, g.Highlights, g.Shadows, g.Whites, g.Blacks, g.Temperature, g.Tint, g.Saturation, g.Vibrance} {
		if !within(v, -MaxGradeSlider, MaxGradeSlider) {
			return CodeOutOfRange
		}
	}
	return ""
}

func (c Clip) gradeIssue() string {
	if c.Color == nil {
		return ""
	}
	if c.Type != ClipVideo && c.Type != ClipImage {
		return CodeInvalid
	}
	return c.Color.issue()
}
