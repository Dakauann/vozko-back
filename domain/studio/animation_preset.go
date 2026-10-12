package studio

import (
	"encoding/json"
	"errors"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"vozko/domain/mediagen"
)

const (
	MaxPresetAnimationBytes   = 1 << 20
	MaxPresetNameRunes        = 120
	MaxPresetDescriptionRunes = 500

	PresetScopeVisual = "visual"
	PresetScopeText   = "text"

	PresetEdgeMotion     = "motion"
	PresetEdgeTransition = "transition"
	PresetEdgeText       = "text"

	presetProbeTrack = "probe-track"
	presetProbeClip  = "probe-clip"
	presetProbeShape = `{"id":"probe-layer","type":"shape","shape":"rect","transform":{"x":0.5,"y":0.5,"w":0.5,"h":0.25,"rotation":0,"opacity":1},"fill":"#ffffff"}`
	presetProbeText  = `{"id":"probe-layer","type":"text","transform":{"x":0.5,"y":0.5,"w":0.5,"h":0.25,"rotation":0,"opacity":1},"text":"Aa","fontId":"inter","fontSize":0.08,"fontWeight":700,"align":"center","lineHeight":1.2,"fill":"#ffffff"}`
)

var presetProbeBox = mediagen.Transform{X: 0.5, Y: 0.5, W: 0.5, H: 0.25, Rotation: 0, Opacity: 1}

var presetLanes = map[string]bool{
	"x": true, "y": true, "position": true, "scale": true, "scaleX": true, "scaleY": true, "rotation": true, "opacity": true, "blur": true,
}

type PresetAnimation struct {
	Scope         string             `json:"scope"`
	DurationMS    int64              `json:"durationMs"`
	Keyframes     *ClipKeyframes     `json:"keyframes,omitempty"`
	Behaviors     []Behavior         `json:"behaviors,omitempty"`
	MotionIn      *mediagen.Motion   `json:"motionIn,omitempty"`
	MotionOut     *mediagen.Motion   `json:"motionOut,omitempty"`
	TransitionIn  *ClipTransition    `json:"transitionIn,omitempty"`
	TransitionOut *ClipTransition    `json:"transitionOut,omitempty"`
	TextIn        *TextAnimator      `json:"textIn,omitempty"`
	TextOut       *TextAnimator      `json:"textOut,omitempty"`
	TextAnimators []TextUnitAnimator `json:"textAnimators,omitempty"`
	Vfx           []Vfx              `json:"vfx,omitempty"`
	MotionBlur    bool               `json:"motionBlur,omitempty"`
}

type PresetSummary struct {
	Scope         string
	DurationMS    int64
	Keys          []string
	Behaviors     []string
	Entrance      string
	Exit          string
	TextAnimators int
	Vfx           []string
	MotionBlur    bool
}

type AnimationPreset struct {
	ID           string
	WorkspaceID  string
	Name         string
	Description  string
	Animation    json.RawMessage
	CreatedBy    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	BuiltIn      bool
	Names        map[string]string
	Descriptions map[string]string
}

type PresetChange struct {
	Name        *string
	Description *string
}

func NewAnimationPreset(workspaceID, createdBy, name, description string, animation json.RawMessage) (*AnimationPreset, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return nil, ErrWorkspaceRequired
	}
	if strings.TrimSpace(createdBy) == "" {
		return nil, ErrCreatorRequired
	}
	cleanName, cleanDescription, err := CleanPresetText(name, description)
	if err != nil {
		return nil, err
	}
	if err := ValidatePresetAnimation(animation); err != nil {
		return nil, err
	}
	return &AnimationPreset{WorkspaceID: workspaceID, CreatedBy: createdBy, Name: cleanName, Description: cleanDescription, Animation: animation}, nil
}

func (p *AnimationPreset) Apply(change PresetChange) error {
	if change.Name == nil && change.Description == nil {
		return issue(FieldName, CodeRequired)
	}
	name, description := p.Name, p.Description
	if change.Name != nil {
		clean, err := cleanLabel(*change.Name, MaxPresetNameRunes)
		if err != nil {
			return err
		}
		name = clean
	}
	if change.Description != nil {
		clean, err := cleanPresetDescription(*change.Description)
		if err != nil {
			return err
		}
		description = clean
	}
	p.Name, p.Description = name, description
	return nil
}

func (p AnimationPreset) Summary() (PresetSummary, error) {
	animation, err := DecodePresetAnimation(p.Animation)
	if err != nil {
		return PresetSummary{}, err
	}
	return animation.Summary(), nil
}

func CleanPresetText(name, description string) (string, string, error) {
	cleanName, err := cleanLabel(name, MaxPresetNameRunes)
	if err != nil {
		return "", "", err
	}
	cleanDescription, err := cleanPresetDescription(description)
	if err != nil {
		return "", "", err
	}
	return cleanName, cleanDescription, nil
}

func cleanPresetDescription(description string) (string, error) {
	clean := strings.TrimSpace(description)
	if utf8.RuneCountInString(clean) > MaxPresetDescriptionRunes {
		return "", issue(FieldDescription, CodeTooLarge)
	}
	return clean, nil
}

func ValidatePresetAnimation(raw []byte) error {
	_, err := DecodePresetAnimation(raw)
	return err
}

func DecodePresetAnimation(raw []byte) (*PresetAnimation, error) {
	if len(raw) == 0 {
		return nil, issue(FieldAnimation, CodeRequired)
	}
	if len(raw) > MaxPresetAnimationBytes {
		return nil, issue(FieldAnimation, CodeTooLarge)
	}
	var animation PresetAnimation
	if err := decodeStrict(raw, &animation); err != nil {
		return nil, issue(FieldAnimation, CodeInvalid)
	}
	if code := animation.issue(); code != "" {
		return nil, issue(FieldAnimation, code)
	}
	return &animation, nil
}

func (a PresetAnimation) issue() string {
	lanes, err := a.Keyframes.laneNames()
	switch {
	case err != nil:
		return CodeInvalid
	case a.Scope != PresetScopeVisual && a.Scope != PresetScopeText:
		return CodeUnknown
	case a.DurationMS < mediagen.MinClipMS || a.DurationMS > MaxVideoMS:
		return CodeOutOfRange
	case slices.ContainsFunc(lanes, func(lane string) bool { return !presetLanes[lane] }):
		return CodeInvalid
	case a.Scope == PresetScopeVisual && a.animatesText():
		return CodeInvalid
	case slices.ContainsFunc(a.Behaviors, func(b Behavior) bool { return b.TrackID != "" }):
		return CodeInvalid
	case !a.carries(lanes):
		return CodeRequired
	}
	return a.probeIssue()
}

func (a PresetAnimation) animatesText() bool {
	return a.TextIn != nil || a.TextOut != nil || len(a.TextAnimators) > 0
}

func (a PresetAnimation) carries(lanes []string) bool {
	return len(lanes) > 0 || len(a.Behaviors) > 0 || a.MotionIn != nil || a.MotionOut != nil || a.TransitionIn != nil || a.TransitionOut != nil ||
		a.animatesText() || len(a.Vfx) > 0 || a.MotionBlur
}

func (a PresetAnimation) probeIssue() string {
	layerJSON := presetProbeShape
	if a.Scope == PresetScopeText {
		layerJSON = presetProbeText
	}
	var layer Layer
	if err := decodeStrict([]byte(layerJSON), &layer); err != nil {
		return CodeInvalid
	}
	clip := Clip{
		ID: presetProbeClip, Type: ClipOverlay, DurationMS: a.DurationMS, Volume: 1, Transform: presetProbeBox, Layer: &layer,
		Keyframes: PresetKeyframesAt(a.Keyframes, presetProbeBox), Behaviors: a.Behaviors,
		MotionIn: a.MotionIn, MotionOut: a.MotionOut, TransitionIn: a.TransitionIn, TransitionOut: a.TransitionOut,
		TextIn: a.TextIn, TextOut: a.TextOut, TextAnimators: a.TextAnimators, Vfx: a.Vfx, MotionBlur: a.MotionBlur,
	}
	doc := VideoDocument{
		Schema: SchemaVideo, Version: DocumentVersion, Canvas: VideoCanvas{Aspect: mediagen.AspectPortrait, Background: "#000000"}, DurationMS: a.DurationMS,
		Tracks: []Track{{ID: presetProbeTrack, Kind: TrackVisual, Clips: []Clip{clip}}},
	}
	err := doc.validate()
	if err == nil {
		return ""
	}
	var invalid *ValidationError
	if errors.As(err, &invalid) && len(invalid.Issues) > 0 {
		return invalid.Issues[0].Code
	}
	return CodeInvalid
}

func PresetKeyframesAt(relative *ClipKeyframes, base mediagen.Transform) *ClipKeyframes {
	if relative == nil {
		return nil
	}
	absolute := *relative
	absolute.X = shiftKeys(relative.X, base.X)
	absolute.Y = shiftKeys(relative.Y, base.Y)
	absolute.Rotation = shiftKeys(relative.Rotation, base.Rotation)
	if relative.Position != nil {
		absolute.Position = make([]PositionKeyframe, len(relative.Position))
		for i, key := range relative.Position {
			key.X += base.X
			key.Y += base.Y
			absolute.Position[i] = key
		}
	}
	return &absolute
}

func shiftKeys(frames []mediagen.Keyframe, by float64) []mediagen.Keyframe {
	if frames == nil {
		return nil
	}
	shifted := make([]mediagen.Keyframe, len(frames))
	for i, key := range frames {
		key.Value += by
		shifted[i] = key
	}
	return shifted
}

func (k *ClipKeyframes) laneNames() ([]string, error) {
	if k == nil {
		return nil, nil
	}
	raw, err := json.Marshal(k)
	if err != nil {
		return nil, err
	}
	var lanes map[string]json.RawMessage
	if err := json.Unmarshal(raw, &lanes); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(lanes))
	for name := range lanes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func (a PresetAnimation) Summary() PresetSummary {
	lanes, _ := a.Keyframes.laneNames()
	kinds := map[string]bool{}
	behaviors := []string{}
	for _, b := range a.Behaviors {
		if !kinds[b.Kind] {
			kinds[b.Kind] = true
			behaviors = append(behaviors, b.Kind)
		}
	}
	sort.Strings(behaviors)
	vfx := make([]string, 0, len(a.Vfx))
	for _, v := range a.Vfx {
		vfx = append(vfx, v.Kind)
	}
	if lanes == nil {
		lanes = []string{}
	}
	return PresetSummary{
		Scope: a.Scope, DurationMS: a.DurationMS, Keys: lanes, Behaviors: behaviors,
		Entrance: presetEdge(a.MotionIn, a.TransitionIn, a.TextIn), Exit: presetEdge(a.MotionOut, a.TransitionOut, a.TextOut),
		TextAnimators: len(a.TextAnimators), Vfx: vfx, MotionBlur: a.MotionBlur,
	}
}

func presetEdge(motion *mediagen.Motion, transition *ClipTransition, text *TextAnimator) string {
	switch {
	case motion != nil:
		return PresetEdgeMotion
	case transition != nil:
		return PresetEdgeTransition
	case text != nil:
		return PresetEdgeText
	}
	return ""
}
