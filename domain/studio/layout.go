package studio

import "math"

const (
	LayoutHorizontal = "horizontal"
	LayoutVertical   = "vertical"
	MaxLayoutSpacing = 10_000.0
	layoutRotation   = 1e-9
)

var (
	layoutAligns  = map[string]bool{"": true, "start": true, "center": true, "end": true}
	layoutSizings = map[string]bool{"hug": true, "fixed": true}
	itemSizings   = map[string]bool{"": true, "fixed": true, "fill": true, "hug": true}
)

type LayoutPadding struct {
	Top    float64 `json:"top"`
	Right  float64 `json:"right"`
	Bottom float64 `json:"bottom"`
	Left   float64 `json:"left"`
}

type Layout struct {
	Direction  string         `json:"direction"`
	Gap        float64        `json:"gap,omitempty"`
	AutoGap    bool           `json:"autoGap,omitempty"`
	Padding    *LayoutPadding `json:"padding,omitempty"`
	AlignMain  string         `json:"alignMain,omitempty"`
	AlignCross string         `json:"alignCross,omitempty"`
	Width      string         `json:"width"`
	Height     string         `json:"height"`
}

type LayoutItem struct {
	Width    string `json:"width,omitempty"`
	Height   string `json:"height,omitempty"`
	Absolute bool   `json:"absolute,omitempty"`
}

func spacingValid(values ...float64) bool {
	for _, v := range values {
		if !within(v, 0, MaxLayoutSpacing) {
			return false
		}
	}
	return true
}

func (l Layout) issue() string {
	switch {
	case l.Direction != LayoutHorizontal && l.Direction != LayoutVertical, !layoutSizings[l.Width] || !layoutSizings[l.Height]:
		return CodeUnknown
	case !layoutAligns[l.AlignMain] || !layoutAligns[l.AlignCross]:
		return CodeUnknown
	case !spacingValid(l.Gap):
		return CodeOutOfRange
	case l.Padding != nil && !spacingValid(l.Padding.Top, l.Padding.Right, l.Padding.Bottom, l.Padding.Left):
		return CodeOutOfRange
	case l.AutoGap && (l.Gap != 0 || l.AlignMain != ""):
		return CodeInvalid
	}
	return ""
}

func (i LayoutItem) sizes(mode string) bool { return i.Width == mode || i.Height == mode }

func (i LayoutItem) issue() string {
	switch {
	case !itemSizings[i.Width] || !itemSizings[i.Height]:
		return CodeUnknown
	case i.Width == "" && i.Height == "" && !i.Absolute:
		return CodeInvalid
	case i.Absolute && (i.sizes("fill") || i.sizes("hug")):
		return CodeInvalid
	}
	return ""
}

func (i LayoutItem) fights(parent Layout) bool {
	return (parent.Width == "hug" && i.Width == "fill") || (parent.Height == "hug" && i.Height == "fill")
}

func (g Group) layoutIssue(groups map[string]Group, layers map[string]Layer) string {
	if g.Layout != nil {
		if g.BaseID == "" || g.Repeat != nil {
			return CodeInvalid
		}
		if code := g.Layout.issue(); code != "" {
			return code
		}
		if math.Abs(layers[g.BaseID].Transform.Rotation) > layoutRotation {
			return CodeInvalid
		}
	}
	if g.LayoutItem == nil {
		return ""
	}
	if code := g.LayoutItem.issue(); code != "" {
		return code
	}
	parent := groups[g.ParentID].Layout
	switch {
	case g.ParentID == "" || parent == nil, g.LayoutItem.sizes("hug"), g.LayoutItem.sizes("fill") && g.Layout == nil, g.LayoutItem.fights(*parent):
		return CodeInvalid
	case g.Layout != nil && ((g.Layout.Width == "hug" && g.LayoutItem.Width == "fill") || (g.Layout.Height == "hug" && g.LayoutItem.Height == "fill")):
		return CodeInvalid
	}
	return ""
}

func (l Layer) layoutIssue(groups map[string]Group) string {
	spec := l.LayoutItem
	if spec == nil {
		return ""
	}
	if code := spec.issue(); code != "" {
		return code
	}
	container, found := groups[l.GroupID]
	switch {
	case l.GroupID == "" || !found || container.Layout == nil || container.BaseID == l.ID, spec.fights(*container.Layout):
		return CodeInvalid
	case (spec.sizes("fill") || spec.sizes("hug")) && (math.Abs(l.Transform.Rotation) > layoutRotation || l.Type == LayerInstance):
		return CodeInvalid
	case spec.sizes("hug") && (l.Type != LayerText || l.Curve != 0):
		return CodeInvalid
	}
	return ""
}

func layoutsIssue(groups []Group, layers []Layer) string {
	byID := make(map[string]Group, len(groups))
	for _, g := range groups {
		byID[g.ID] = g
	}
	layerByID := make(map[string]Layer, len(layers))
	for _, l := range layers {
		layerByID[l.ID] = l
	}
	for _, g := range groups {
		if code := g.layoutIssue(byID, layerByID); code != "" {
			return code
		}
	}
	for _, l := range layers {
		if code := l.layoutIssue(byID); code != "" {
			return code
		}
	}
	return ""
}
