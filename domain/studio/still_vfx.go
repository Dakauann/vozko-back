package studio

var stillVfxParams = map[string][]string{
	"grain":               {"amount", "size", "chroma"},
	"lightLeak":           {"amount", "size", "x", "y"},
	"vignette":            {"amount", "size", "softness", "roundness"},
	"chromaticAberration": {"amount", "angle", "radial"},
	"glitch":              {"amount", "size", "split", "scanlines"},
	"lensFlare":           {"amount", "size", "x", "y", "ghosts", "streak"},
	"glow":                {"amount", "threshold", "size"},
	"mosaic":              {"amount", "size"},
	"posterize":           {"amount", "levels"},
	"mirror":              {"amount", "x", "y", "angle"},
	"offset":              {"amount", "shiftX", "shiftY"},
	"directionalBlur":     {"amount", "angle", "length"},
	"zoomBlur":            {"amount", "x", "y", "length"},
	"spinBlur":            {"amount", "x", "y", "angle"},
	"waveWarp":            {"amount", "angle", "length", "height", "phase"},
	"ripple":              {"amount", "size", "x", "y", "length", "height", "phase"},
	"twirl":               {"amount", "size", "x", "y", "angle"},
	"bulge":               {"amount", "size", "x", "y", "height"},
	"cornerPin":           {"amount", "topLeftX", "topLeftY", "topRightX", "topRightY", "bottomRightX", "bottomRightY", "bottomLeftX", "bottomLeftY"},
	"fractalNoise":        {"amount", "size", "complexity", "contrast", "brightness"},
	"gradientRamp":        {"amount", "x", "y", "radial", "toX", "toY"},
	"fourColorGradient":   {"amount", "softness", "topLeftX", "topLeftY", "topRightX", "topRightY", "bottomRightX", "bottomRightY", "bottomLeftX", "bottomLeftY"},
}

func StillVfxKinds() []string {
	out := make([]string, 0, len(stillVfxParams))
	for kind := range stillVfxParams {
		out = append(out, kind)
	}
	return out
}

func StillVfxParams(kind string) []string {
	return append([]string(nil), stillVfxParams[kind]...)
}

func stillParam(kind, name string) bool {
	for _, allowed := range stillVfxParams[kind] {
		if allowed == name {
			return true
		}
	}
	return false
}

func (v Vfx) stillIssue() string {
	if _, still := stillVfxParams[v.Kind]; !still {
		return CodeUnknown
	}
	if v.OffsetMS != 0 {
		return CodeInvalid
	}
	for _, frames := range v.Keyframes {
		if len(frames) > 0 {
			return CodeInvalid
		}
	}
	for name, value := range v.Params {
		if value != nil && !stillParam(v.Kind, name) {
			return CodeInvalid
		}
	}
	return ""
}

func stillVfxIssue(list []Vfx) string {
	if code := vfxIssue(list); code != "" {
		return code
	}
	for _, v := range list {
		if code := v.stillIssue(); code != "" {
			return code
		}
	}
	return ""
}
