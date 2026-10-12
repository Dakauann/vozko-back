package studio

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"vozko/domain/mediagen"
)

const (
	SchemaImage           = "studio.image"
	SchemaVideo           = "studio.video"
	DocumentVersion       = 1
	ImageDocumentVersion  = 2
	MaxArtboardCoordinate = 100_000
	MaxDocumentBytes      = 10 << 20
	MaxVideoMS            = 600_000
	MinCanvasSide         = 100
	MaxCanvasSide         = 4096
	MaxMarkerLabelRunes   = 64
)

type Canvas struct {
	Width             int       `json:"width"`
	Height            int       `json:"height"`
	Background        string    `json:"background"`
	Gradient          *Gradient `json:"gradient,omitempty"`
	BackgroundStyleID string    `json:"backgroundStyleId,omitempty"`
	Vfx               []Vfx     `json:"vfx,omitempty"`
}

type Group struct {
	ID         string      `json:"id"`
	ParentID   string      `json:"parentId,omitempty"`
	Name       string      `json:"name,omitempty"`
	BaseID     string      `json:"baseId,omitempty"`
	Repeat     *Repeat     `json:"repeat,omitempty"`
	Layout     *Layout     `json:"layout,omitempty"`
	LayoutItem *LayoutItem `json:"layoutItem,omitempty"`
}

type LegacyImageDocument struct {
	Schema  string  `json:"schema"`
	Version int     `json:"version"`
	Canvas  Canvas  `json:"canvas"`
	Layers  []Layer `json:"layers"`
	Groups  []Group `json:"groups,omitempty"`
}

type Artboard struct {
	ID        string  `json:"id"`
	Name      string  `json:"name,omitempty"`
	Component bool    `json:"component,omitempty"`
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Canvas    Canvas  `json:"canvas"`
	Layers    []Layer `json:"layers"`
	Groups    []Group `json:"groups,omitempty"`
}

type ImageDocument struct {
	Schema    string     `json:"schema"`
	Version   int        `json:"version"`
	Artboards []Artboard `json:"artboards"`
	Styles    []Style    `json:"styles,omitempty"`
}

type TrackKind string

const (
	TrackVisual TrackKind = "visual"
	TrackAudio  TrackKind = "audio"
)

type ClipType string

const (
	ClipVideo   ClipType = "video"
	ClipImage   ClipType = "image"
	ClipOverlay ClipType = "overlay"
	ClipAudio   ClipType = "audio"
)

type Clip struct {
	ID            string             `json:"id"`
	Type          ClipType           `json:"type"`
	Name          string             `json:"name,omitempty"`
	AssetID       string             `json:"assetId,omitempty"`
	StartMS       int64              `json:"startMs"`
	DurationMS    int64              `json:"durationMs"`
	TrimInMS      int64              `json:"trimInMs"`
	Volume        float64            `json:"volume"`
	FadeInMS      int64              `json:"fadeInMs"`
	FadeOutMS     int64              `json:"fadeOutMs"`
	Pan           float64            `json:"pan,omitempty"`
	Fit           mediagen.Fit       `json:"fit,omitempty"`
	Transform     mediagen.Transform `json:"transform"`
	Layer         *Layer             `json:"layer,omitempty"`
	LinkID        string             `json:"linkId,omitempty"`
	MotionIn      *mediagen.Motion   `json:"motionIn,omitempty"`
	MotionOut     *mediagen.Motion   `json:"motionOut,omitempty"`
	TransitionIn  *ClipTransition    `json:"transitionIn,omitempty"`
	TransitionOut *ClipTransition    `json:"transitionOut,omitempty"`
	Disabled      bool               `json:"disabled,omitempty"`
	Keyframes     *ClipKeyframes     `json:"keyframes,omitempty"`
	Blur          float64            `json:"blur,omitempty"`
	TextIn        *TextAnimator      `json:"textIn,omitempty"`
	TextOut       *TextAnimator      `json:"textOut,omitempty"`
	TextAnimators []TextUnitAnimator `json:"textAnimators,omitempty"`
	Speed         *float64           `json:"speed,omitempty"`
	Reverse       bool               `json:"reverse,omitempty"`
	Freeze        bool               `json:"freeze,omitempty"`
	PreservePitch *bool              `json:"preservePitch,omitempty"`
	FrameMix      bool               `json:"frameMix,omitempty"`
	Color         *ColorGrade        `json:"color,omitempty"`
	GradientMap   *GradientMap       `json:"gradientMap,omitempty"`
	ChromaKey     *ChromaKey         `json:"chromaKey,omitempty"`
	LumaKey       *LumaKey           `json:"lumaKey,omitempty"`
	Masks         []Mask             `json:"masks,omitempty"`
	Effects       []Effect           `json:"effects,omitempty"`
	Strokes       []StrokePaint      `json:"strokes,omitempty"`
	Matte         bool               `json:"matte,omitempty"`
	MatteChannel  string             `json:"matteChannel,omitempty"`
	MatteInvert   bool               `json:"matteInvert,omitempty"`
	BlendMode     string             `json:"blendMode,omitempty"`
	Line          *LineAnimation     `json:"line,omitempty"`
	Vfx           []Vfx              `json:"vfx,omitempty"`
	Anchor        *Anchor            `json:"anchor,omitempty"`
	FlipX         bool               `json:"flipX,omitempty"`
	FlipY         bool               `json:"flipY,omitempty"`
	AutoOrient    bool               `json:"autoOrient,omitempty"`
	MotionBlur    bool               `json:"motionBlur,omitempty"`
	RotationX     float64            `json:"rotationX,omitempty"`
	RotationY     float64            `json:"rotationY,omitempty"`
	Camera        *ClipCamera        `json:"camera,omitempty"`
	Counter       *Counter           `json:"counter,omitempty"`
	TextPath      *TextPath          `json:"textPath,omitempty"`
	Repeat        *ClipRepeat        `json:"repeat,omitempty"`
	ParentID      string             `json:"parentId,omitempty"`
	ParentTracker string             `json:"parentTracker,omitempty"`
	Trackers      []PointTracker     `json:"trackers,omitempty"`
	Stabilize     *Stabilize         `json:"stabilize,omitempty"`
	Behaviors     []Behavior         `json:"behaviors,omitempty"`
	LabelColor    string             `json:"labelColor,omitempty"`
	Markers       []ClipMarker       `json:"markers,omitempty"`
	Guide         bool               `json:"guide,omitempty"`
	Tracks        []Track            `json:"tracks,omitempty"`
	ChartIn       *ChartAnimator     `json:"chartIn,omitempty"`
}

type Track struct {
	ID          string       `json:"id"`
	Kind        TrackKind    `json:"kind"`
	Name        string       `json:"name,omitempty"`
	Hidden      bool         `json:"hidden,omitempty"`
	Locked      bool         `json:"locked,omitempty"`
	Muted       bool         `json:"muted,omitempty"`
	Clips       []Clip       `json:"clips"`
	Transitions []Transition `json:"transitions,omitempty"`
	GainDB      *float64     `json:"gainDb,omitempty"`
	Pan         *float64     `json:"pan,omitempty"`
	Ducking     *DuckingRule `json:"ducking,omitempty"`
	LabelColor  string       `json:"labelColor,omitempty"`
	Shy         bool         `json:"shy,omitempty"`
}

type VideoCanvas struct {
	Aspect       mediagen.Aspect `json:"aspect"`
	Background   string          `json:"background"`
	ShutterAngle *float64        `json:"shutterAngle,omitempty"`
	ShutterPhase *float64        `json:"shutterPhase,omitempty"`
	Perspective  *float64        `json:"perspective,omitempty"`
}

type VideoDocument struct {
	Schema     string      `json:"schema"`
	Version    int         `json:"version"`
	Canvas     VideoCanvas `json:"canvas"`
	DurationMS int64       `json:"durationMs"`
	Tracks     []Track     `json:"tracks"`
	Markers    []Marker    `json:"markers,omitempty"`
}

type Marker struct {
	ID         string `json:"id"`
	AtMS       int64  `json:"atMs"`
	DurationMS int64  `json:"durationMs,omitempty"`
	Label      string `json:"label,omitempty"`
	Color      string `json:"color,omitempty"`
	Note       string `json:"note,omitempty"`
}

func decodeStrict(raw []byte, into any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil || dec.More() {
		return issue(FieldDocument, CodeInvalid)
	}
	return nil
}

func ValidateDocument(kind Kind, raw []byte) error {
	if len(raw) == 0 {
		return issue(FieldDocument, CodeRequired)
	}
	if len(raw) > MaxDocumentBytes {
		return issue(FieldDocument, CodeTooLarge)
	}
	switch kind {
	case KindImage:
		return validateImage(raw)
	case KindVideo:
		var doc VideoDocument
		if err := decodeStrict(raw, &doc); err != nil {
			return err
		}
		return doc.validate()
	}
	return issue(FieldKind, CodeUnknown)
}

type seenIDs struct {
	layers map[string]bool
	groups map[string]bool
}

func freshIDs() seenIDs {
	return seenIDs{layers: map[string]bool{}, groups: map[string]bool{}}
}

func validateImage(raw []byte) error {
	var head struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return issue(FieldDocument, CodeInvalid)
	}
	if head.Version == DocumentVersion {
		var legacy LegacyImageDocument
		if err := decodeStrict(raw, &legacy); err != nil {
			return err
		}
		return legacy.validate()
	}
	var doc ImageDocument
	if err := decodeStrict(raw, &doc); err != nil {
		return err
	}
	return doc.validate()
}

func (d LegacyImageDocument) validate() error {
	if d.Schema != SchemaImage || d.Version != DocumentVersion {
		return issue(FieldDocument, CodeUnknown)
	}
	if code := componentsIssue([]Artboard{{Canvas: d.Canvas, Layers: d.Layers}}); code != "" {
		return issue(FieldLayers, code)
	}
	if code := styleLinksIssue([]Artboard{{Canvas: d.Canvas, Layers: d.Layers}}, nil); code != "" {
		return issue(FieldLayers, code)
	}
	return surfaceIssue(d.Canvas, d.Layers, d.Groups, freshIDs())
}

func (d ImageDocument) validate() error {
	switch {
	case d.Schema != SchemaImage || d.Version != ImageDocumentVersion:
		return issue(FieldDocument, CodeUnknown)
	case len(d.Artboards) == 0:
		return issue(FieldArtboards, CodeRequired)
	}
	seen := freshIDs()
	artboards := make(map[string]bool, len(d.Artboards))
	for _, a := range d.Artboards {
		if err := a.issue(artboards); err != nil {
			return err
		}
		if err := surfaceIssue(a.Canvas, a.Layers, a.Groups, seen); err != nil {
			return err
		}
	}
	if code := componentsIssue(d.Artboards); code != "" {
		return issue(FieldLayers, code)
	}
	if code := stylesIssue(d.Styles); code != "" {
		return issue(FieldStyles, code)
	}
	if code := styleLinksIssue(d.Artboards, d.Styles); code != "" {
		return issue(FieldLayers, code)
	}
	return nil
}

func (a Artboard) issue(seen map[string]bool) error {
	switch {
	case !validToken(a.ID) || utf8.RuneCountInString(a.Name) > maxTokenRunes*2:
		return issue(FieldArtboards, CodeInvalid)
	case seen[a.ID]:
		return issue(FieldArtboards, CodeDuplicate)
	case !within(a.X, -MaxArtboardCoordinate, MaxArtboardCoordinate) || !within(a.Y, -MaxArtboardCoordinate, MaxArtboardCoordinate):
		return issue(FieldArtboards, CodeOutOfRange)
	}
	seen[a.ID] = true
	return nil
}

func surfaceIssue(canvas Canvas, layers []Layer, groups []Group, seen seenIDs) error {
	switch {
	case canvas.Width < MinCanvasSide || canvas.Width > MaxCanvasSide || canvas.Height < MinCanvasSide || canvas.Height > MaxCanvasSide:
		return issue(FieldCanvas, CodeOutOfRange)
	case !validColor(canvas.Background, false) || !canvas.Gradient.valid():
		return issue(FieldCanvas, CodeInvalid)
	}
	if code := stillVfxIssue(canvas.Vfx); code != "" {
		return issue(FieldCanvas, code)
	}
	if err := layersIssue(layers, seen.layers); err != nil {
		return err
	}
	code := groupsIssue(groups, layers)
	if code == "" {
		code = repeatsIssue(groups, layers)
	}
	if code == "" {
		code = layoutsIssue(groups, layers)
	}
	if code != "" {
		return issue(FieldLayers, code)
	}
	for _, g := range groups {
		if seen.groups[g.ID] {
			return issue(FieldLayers, CodeDuplicate)
		}
		seen.groups[g.ID] = true
	}
	return nil
}

func groupsIssue(groups []Group, layers []Layer) string {
	homes := make(map[string]string, len(layers))
	for _, l := range layers {
		homes[l.ID] = l.GroupID
	}
	parents := make(map[string]string, len(groups))
	for _, g := range groups {
		if !validToken(g.ID) || (g.ParentID != "" && !validToken(g.ParentID)) || utf8.RuneCountInString(g.Name) > maxTokenRunes*2 {
			return CodeInvalid
		}
		if home, found := homes[g.BaseID]; g.BaseID != "" && (!found || home != g.ID) {
			return CodeInvalid
		}
		if _, seen := parents[g.ID]; seen {
			return CodeDuplicate
		}
		parents[g.ID] = g.ParentID
	}
	for _, g := range groups {
		steps := 0
		for at := g.ParentID; at != ""; at = parents[at] {
			if _, known := parents[at]; !known || at == g.ID || steps > len(groups) {
				return CodeInvalid
			}
			steps++
		}
	}
	return ""
}

func layersIssue(layers []Layer, seen map[string]bool) error {
	for _, l := range layers {
		if seen[l.ID] {
			return issue(FieldLayers, CodeDuplicate)
		}
		seen[l.ID] = true
		if code := l.issue(); code != "" {
			return issue(FieldLayers, code)
		}
		if code := l.stillPathOpsIssue(); code != "" {
			return issue(FieldLayers, code)
		}
	}
	return nil
}

func (d VideoDocument) validate() error {
	switch {
	case d.Schema != SchemaVideo || d.Version != DocumentVersion:
		return issue(FieldDocument, CodeUnknown)
	case !validColor(d.Canvas.Background, true) || len(d.Canvas.Background) != 7:
		return issue(FieldCanvas, CodeInvalid)
	case d.DurationMS < 0 || d.DurationMS > MaxVideoMS:
		return issue(FieldTracks, CodeOutOfRange)
	}
	if _, err := d.Canvas.Aspect.Size(); err != nil {
		return issue(FieldCanvas, CodeUnknown)
	}
	if code := d.Canvas.shutterIssue(); code != "" {
		return issue(FieldCanvas, code)
	}
	if code := d.Canvas.perspectiveIssue(); code != "" {
		return issue(FieldCanvas, code)
	}
	if err := d.timelineIssue(map[string]bool{}); err != nil {
		return err
	}
	return markersIssue(d.Markers)
}

func (d VideoDocument) timelineIssue(ids map[string]bool) error {
	for _, track := range d.Tracks {
		if !validToken(track.ID) || ids[track.ID] || (track.Kind != TrackVisual && track.Kind != TrackAudio) {
			return issue(FieldTracks, CodeInvalid)
		}
		if code := labelColorIssue(track.LabelColor); code != "" {
			return issue(FieldTracks, code)
		}
		ids[track.ID] = true
		if err := track.validate(d, ids); err != nil {
			return err
		}
	}
	if code := d.parentsIssue(); code != "" {
		return issue(FieldTracks, code)
	}
	if code := d.behaviorTracksIssue(); code != "" {
		return issue(FieldTracks, code)
	}
	if code := d.vfxTracksIssue(); code != "" {
		return issue(FieldTracks, code)
	}
	for _, track := range d.Tracks {
		if code := track.transitionsIssue(ids); code != "" {
			return issue(FieldTracks, code)
		}
		if code := d.trackSoundIssue(track); code != "" {
			return issue(FieldTracks, code)
		}
	}
	return nil
}

func markersIssue(markers []Marker) error {
	if code := markerListIssue(markers, MaxVideoMS); code != "" {
		return issue(FieldMarkers, code)
	}
	return nil
}

func (t Track) validate(timeline VideoDocument, ids map[string]bool) error {
	var lastEnd int64
	for i, c := range t.Clips {
		if !validToken(c.ID) || ids[c.ID] {
			return issue(FieldTracks, CodeInvalid)
		}
		ids[c.ID] = true
		if code := c.issue(t.Kind, timeline.DurationMS); code != "" {
			return issue(FieldTracks, code)
		}
		if i > 0 && c.StartMS < lastEnd {
			return issue(FieldTracks, CodeOverlap)
		}
		lastEnd = c.StartMS + c.DurationMS
		if c.Type != ClipGroup {
			continue
		}
		if err := timeline.groupTimeline(c).timelineIssue(ids); err != nil {
			return err
		}
	}
	return nil
}

func (c Clip) issue(kind TrackKind, durationMS int64) string {
	switch {
	case c.StartMS < 0 || c.TrimInMS < 0 || c.DurationMS < mediagen.MinClipMS || c.StartMS+c.DurationMS > durationMS:
		return CodeOutOfRange
	case c.FadeInMS < 0 || c.FadeOutMS < 0 || c.FadeInMS+c.FadeOutMS > c.DurationMS:
		return CodeOutOfRange
	case !within(c.Volume, 0, mediagen.MaxVolume):
		return CodeOutOfRange
	case c.LinkID != "" && !validToken(c.LinkID):
		return CodeInvalid
	case c.Line != nil && c.Type != ClipOverlay:
		return CodeInvalid
	case (c.Counter != nil || c.TextPath != nil || c.Repeat != nil) && c.Type != ClipOverlay:
		return CodeInvalid
	case (c.TransitionIn != nil || c.TransitionOut != nil) && c.Type != ClipVideo && c.Type != ClipImage && c.Type != ClipOverlay && c.Type != ClipGroup:
		return CodeInvalid
	}
	if code := c.soundAssetIssue(); code != "" {
		return code
	}
	if code := c.trackingIssue(); code != "" {
		return code
	}
	if code := c.chartInIssue(); code != "" {
		return code
	}
	if code := c.nestingIssue(); code != "" {
		return code
	}
	if code := c.organizeIssue(); code != "" {
		return code
	}
	if code := c.behaviorsIssue(); code != "" {
		return code
	}
	if code := c.styleIssue(); code != "" {
		return code
	}
	if kind == TrackAudio {
		if c.Type != ClipAudio || strings.TrimSpace(c.AssetID) == "" || c.MotionIn != nil || c.MotionOut != nil || !c.Keyframes.soundOnly() || c.Blur != 0 || c.TextIn != nil || c.TextOut != nil || len(c.TextAnimators) > 0 || c.Color != nil || c.GradientMap != nil || c.ChromaKey != nil || c.LumaKey != nil || len(c.Masks) > 0 || c.Matte || c.MatteChannel != "" || c.MatteInvert || c.BlendMode != "" || len(c.Vfx) > 0 || c.placed() || c.MotionBlur || c.Camera != nil || c.carriesTilt() {
			return CodeInvalid
		}
		if code := c.panIssue(kind); code != "" {
			return code
		}
		if code := c.Keyframes.issue(c.Transform); code != "" {
			return code
		}
		return c.speedIssue()
	}
	if c.Keyframes.hasSound() || c.panIssue(kind) != "" {
		return CodeInvalid
	}
	if code := c.compositingIssue(); code != "" {
		return code
	}
	if code := c.tiltIssue(); code != "" {
		return code
	}
	switch c.Type {
	case ClipVideo, ClipImage:
		if strings.TrimSpace(c.AssetID) == "" || (c.Fit != mediagen.FitCover && c.Fit != mediagen.FitContain) {
			return CodeInvalid
		}
	case ClipOverlay:
		if c.Layer == nil || c.AssetID != "" || c.Layer.carriesPixels() || c.Layer.Type == LayerInstance || c.Layer.linksStyles() || c.Layer.LayoutItem != nil || len(c.Layer.Vfx) > 0 {
			return CodeInvalid
		}
		if code := effectKeysIssue(c.Layer.Effects); code != "" {
			return code
		}
		if code := c.Layer.withoutEffectKeys().issueWithin(c.runLimit()); code != "" {
			return code
		}
	case ClipEffect:
		if code := c.effectIssue(); code != "" {
			return code
		}
		return effectVfxIssue(c.Vfx)
	case ClipNull:
		if code := c.nullIssue(); code != "" {
			return code
		}
	case ClipGroup:
		if code := c.groupIssue(); code != "" {
			return code
		}
	default:
		return CodeUnknown
	}
	if code := mediagen.MotionIssue(c.MotionIn, c.MotionOut, c.DurationMS); code != "" {
		return code
	}
	if code := c.edgeTransitionsIssue(); code != "" {
		return code
	}
	if !within(c.Blur, 0, MaxClipBlur) {
		return CodeOutOfRange
	}
	if code := c.Keyframes.issue(c.Transform); code != "" {
		return code
	}
	if code := c.Keyframes.paintIssue(c); code != "" {
		return code
	}
	if code := c.morphIssue(); code != "" {
		return code
	}
	if code := c.textAnimationIssue(); code != "" {
		return code
	}
	if code := c.textAnimatorsIssue(); code != "" {
		return code
	}
	if code := c.speedIssue(); code != "" {
		return code
	}
	if code := c.colorFinishIssue(); code != "" {
		return code
	}
	if code := c.gradeIssue(); code != "" {
		return code
	}
	if code := c.chromaIssue(); code != "" {
		return code
	}
	if code := c.placementIssue(); code != "" {
		return code
	}
	if code := c.motionBlurIssue(); code != "" {
		return code
	}
	if code := c.cameraIssue(); code != "" {
		return code
	}
	if code := masksIssue(c.Masks); code != "" {
		return code
	}
	if code := c.lineIssue(); code != "" {
		return code
	}
	if code := c.counterIssue(); code != "" {
		return code
	}
	if code := c.textPathIssue(); code != "" {
		return code
	}
	if code := c.Repeat.issue(); code != "" {
		return code
	}
	if code := vfxIssue(c.Vfx); code != "" {
		return code
	}
	low, high := c.positionRange()
	return studioTransformIssue(c.Transform, low, high)
}

func studioTransformIssue(t mediagen.Transform, low, high float64) string {
	switch {
	case !within(t.X, low, high) || !within(t.Y, low, high) || !within(t.Opacity, 0, 1):
		return CodeOutOfRange
	case !within(t.W, 0.001, 4) || !within(t.H, 0.001, 4) || !within(t.Rotation, -360, 360):
		return CodeOutOfRange
	}
	return ""
}
