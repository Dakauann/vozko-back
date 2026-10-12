package studio

import (
	"reflect"
	"strings"
)

type InstanceOverride struct {
	LayerID    string `json:"layerId"`
	Text       string `json:"text,omitempty"`
	Fill       string `json:"fill,omitempty"`
	Stroke     string `json:"stroke,omitempty"`
	FontID     string `json:"fontId,omitempty"`
	FontWeight int    `json:"fontWeight,omitempty"`
	Italic     *bool  `json:"italic,omitempty"`
	AssetID    string `json:"assetId,omitempty"`
	Crop       *Crop  `json:"crop,omitempty"`
	IconID     string `json:"iconId,omitempty"`
	Hidden     *bool  `json:"hidden,omitempty"`
}

var instanceFields = map[string]bool{
	"id": true, "type": true, "name": true, "transform": true, "hidden": true, "locked": true,
	"groupId": true, "flipX": true, "flipY": true, "componentId": true, "overrides": true, "layoutItem": true,
}

func jsonName(field reflect.StructField) string {
	name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
	return name
}

func (l Layer) instanceIssue() string {
	if !l.setsOnly(instanceFields) || !validToken(l.ComponentID) {
		return CodeInvalid
	}
	return ""
}

func (o InstanceOverride) fields() []string {
	var set []string
	add := func(name string, present bool) {
		if present {
			set = append(set, name)
		}
	}
	add("text", o.Text != "")
	add("fill", o.Fill != "")
	add("stroke", o.Stroke != "")
	add("fontId", o.FontID != "")
	add("fontWeight", o.FontWeight != 0)
	add("italic", o.Italic != nil)
	add("assetId", o.AssetID != "")
	add("crop", o.Crop != nil)
	add("iconId", o.IconID != "")
	add("hidden", o.Hidden != nil)
	return set
}

func overridable(main Layer, field string) bool {
	switch field {
	case "text", "fontId", "fontWeight", "italic":
		return main.Type == LayerText
	case "assetId", "crop":
		return main.Type == LayerImage
	case "iconId":
		return main.Type == LayerIcon
	case "fill":
		return main.paintsFill()
	case "stroke":
		return main.paintsStroke()
	case "hidden":
		return true
	}
	return false
}

func (o InstanceOverride) applied(main Layer) Layer {
	merged := main
	if o.Text != "" {
		merged.Text = o.Text
	}
	if o.Fill != "" {
		merged.Fill = o.Fill
	}
	if o.Stroke != "" {
		merged.Stroke = o.Stroke
	}
	if o.FontID != "" {
		merged.FontID = o.FontID
	}
	if o.FontWeight != 0 {
		merged.FontWeight = o.FontWeight
	}
	if o.Italic != nil {
		merged.Italic = *o.Italic
	}
	if o.AssetID != "" {
		merged.AssetID = o.AssetID
	}
	if o.Crop != nil {
		merged.Crop = o.Crop
	}
	if o.IconID != "" {
		merged.IconID = o.IconID
	}
	if o.Hidden != nil {
		merged.Hidden = *o.Hidden
	}
	return merged
}

func overridesIssue(overrides []InstanceOverride, component Artboard) string {
	mains := make(map[string]Layer, len(component.Layers))
	for _, l := range component.Layers {
		mains[l.ID] = l
	}
	seen := map[string]bool{}
	for _, o := range overrides {
		main, found := mains[o.LayerID]
		if !found {
			return CodeInvalid
		}
		if seen[o.LayerID] {
			return CodeDuplicate
		}
		seen[o.LayerID] = true
		fields := o.fields()
		if len(fields) == 0 {
			return CodeInvalid
		}
		for _, field := range fields {
			if !overridable(main, field) {
				return CodeInvalid
			}
		}
		if code := o.applied(main).issue(); code != "" {
			return code
		}
	}
	return ""
}

func componentsIssue(artboards []Artboard) string {
	components := map[string]Artboard{}
	for _, a := range artboards {
		if a.Component {
			components[a.ID] = a
		}
	}
	for _, a := range artboards {
		for _, l := range a.Layers {
			if l.Type != LayerInstance {
				continue
			}
			component, found := components[l.ComponentID]
			if a.Component || !found {
				return CodeInvalid
			}
			if code := overridesIssue(l.Overrides, component); code != "" {
				return code
			}
		}
	}
	return ""
}
