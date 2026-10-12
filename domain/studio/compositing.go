package studio

import (
	"slices"

	"vozko/domain/mediagen"
)

const (
	LumaDark  = "dark"
	LumaLight = "light"

	MinLumaThreshold = 0.0
	MinLumaSoftness  = 0.001
	MaxLumaValue     = 1.0

	MatteAlpha = "alpha"
	MatteLuma  = "luma"
)

var (
	blendModeList    = []string{"normal", "multiply", "screen", "overlay", "darken", "lighten", "color-dodge", "add", "color-burn", "hard-light", "soft-light", "difference", "exclusion", "hue", "saturation", "color", "luminosity"}
	lumaKeyModeList  = []string{LumaDark, LumaLight}
	matteChannelList = []string{MatteAlpha, MatteLuma}
)

func BlendModes() []string { return slices.Clone(blendModeList) }

func LumaKeyModes() []string { return slices.Clone(lumaKeyModeList) }

func MatteChannels() []string { return slices.Clone(matteChannelList) }

func blendModeSet() map[string]bool {
	set := map[string]bool{"": true}
	for _, mode := range blendModeList {
		set[mode] = true
	}
	return set
}

type LumaKeyframes struct {
	Threshold []mediagen.Keyframe `json:"threshold,omitempty"`
	Softness  []mediagen.Keyframe `json:"softness,omitempty"`
}

type LumaKey struct {
	Mode      string         `json:"mode"`
	Threshold float64        `json:"threshold"`
	Softness  float64        `json:"softness"`
	Keyframes *LumaKeyframes `json:"keyframes,omitempty"`
}

func (k *LumaKeyframes) issue() string {
	if k == nil {
		return ""
	}
	if code := keysIssue(k.Threshold, MinLumaThreshold, MaxLumaValue); code != "" {
		return code
	}
	return keysIssue(k.Softness, MinLumaSoftness, MaxLumaValue)
}

func (k LumaKey) issue() string {
	if !slices.Contains(lumaKeyModeList, k.Mode) {
		return CodeUnknown
	}
	if !within(k.Threshold, MinLumaThreshold, MaxLumaValue) || !within(k.Softness, MinLumaSoftness, MaxLumaValue) {
		return CodeOutOfRange
	}
	return k.Keyframes.issue()
}

func (c Clip) media() bool { return c.Type == ClipVideo || c.Type == ClipImage }

func (c Clip) blendsAsMedia() bool { return c.media() || c.Type == ClipGroup }

func (c Clip) compositingIssue() string {
	if c.BlendMode != "" && !blendModes[c.BlendMode] {
		return CodeUnknown
	}
	if c.MatteChannel != "" && !slices.Contains(matteChannelList, c.MatteChannel) {
		return CodeUnknown
	}
	if (c.BlendMode != "" && !c.blendsAsMedia()) || (c.LumaKey != nil && !c.media()) {
		return CodeInvalid
	}
	if (c.MatteChannel != "" || c.MatteInvert) && !c.Matte {
		return CodeInvalid
	}
	if c.LumaKey != nil {
		return c.LumaKey.issue()
	}
	return ""
}
