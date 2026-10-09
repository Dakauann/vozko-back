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
	MaxDocumentBytes      = 2 << 20
	MinCanvasSide         = 100
	MaxCanvasSide         = 4096
	MaxMarkers            = 50
	MaxMarkerLabelRunes   = 64
)

var fonts = map[string]bool{
	"inter": true, "roboto": true, "open-sans": true, "montserrat": true, "poppins": true, "lato": true, "nunito": true,
	"raleway": true, "oswald": true, "playfair-display": true, "merriweather": true, "bebas-neue": true, "dm-sans": true, "work-sans": true,
}

func Fonts() []string {
	out := make([]string, 0, len(fonts))
	for id := range fonts {
		out = append(out, id)
	}
	return out
}

type Canvas struct {
	Width      int       `json:"width"`
	Height     int       `json:"height"`
	Background string    `json:"background"`
	Gradient   *Gradient `json:"gradient,omitempty"`
}

type Group struct {
	ID       string `json:"id"`
	ParentID string `json:"parentId,omitempty"`
	Name     string `json:"name,omitempty"`
	BaseID   string `json:"baseId,omitempty"`
}

type LegacyImageDocument struct {
	Schema  string  `json:"schema"`
	Version int     `json:"version"`
	Canvas  Canvas  `json:"canvas"`
	Layers  []Layer `json:"layers"`
	Groups  []Group `json:"groups,omitempty"`
}

type Artboard struct {
	ID     string  `json:"id"`
	Name   string  `json:"name,omitempty"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Canvas Canvas  `json:"canvas"`
	Layers []Layer `json:"layers"`
	Groups []Group `json:"groups,omitempty"`
}

type ImageDocument struct {
	Schema    string     `json:"schema"`
	Version   int        `json:"version"`
	Artboards []Artboard `json:"artboards"`
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
	ID         string              `json:"id"`
	Type       ClipType            `json:"type"`
	AssetID    string              `json:"assetId,omitempty"`
	StartMS    int64               `json:"startMs"`
	DurationMS int64               `json:"durationMs"`
	TrimInMS   int64               `json:"trimInMs"`
	Volume     float64             `json:"volume"`
	FadeInMS   int64               `json:"fadeInMs"`
	FadeOutMS  int64               `json:"fadeOutMs"`
	Fit        mediagen.Fit        `json:"fit,omitempty"`
	Transform  mediagen.Transform  `json:"transform"`
	Layer      *Layer              `json:"layer,omitempty"`
	LinkID     string              `json:"linkId,omitempty"`
	MotionIn   *mediagen.Motion    `json:"motionIn,omitempty"`
	MotionOut  *mediagen.Motion    `json:"motionOut,omitempty"`
	Disabled   bool                `json:"disabled,omitempty"`
	Keyframes  *mediagen.Keyframes `json:"keyframes,omitempty"`
}

type Track struct {
	ID     string    `json:"id"`
	Kind   TrackKind `json:"kind"`
	Name   string    `json:"name,omitempty"`
	Hidden bool      `json:"hidden,omitempty"`
	Locked bool      `json:"locked,omitempty"`
	Muted  bool      `json:"muted,omitempty"`
	Clips  []Clip    `json:"clips"`
}

type VideoCanvas struct {
	Aspect     mediagen.Aspect `json:"aspect"`
	Background string          `json:"background"`
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
	ID    string `json:"id"`
	AtMS  int64  `json:"atMs"`
	Label string `json:"label,omitempty"`
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
	if err := layersIssue(layers, seen.layers); err != nil {
		return err
	}
	if code := groupsIssue(groups, layers); code != "" {
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
	}
	return nil
}

func (d VideoDocument) validate() error {
	switch {
	case d.Schema != SchemaVideo || d.Version != DocumentVersion:
		return issue(FieldDocument, CodeUnknown)
	case !validColor(d.Canvas.Background, true) || len(d.Canvas.Background) != 7:
		return issue(FieldCanvas, CodeInvalid)
	case d.DurationMS < 0 || d.DurationMS > mediagen.MaxVideoMS:
		return issue(FieldTracks, CodeOutOfRange)
	}
	if _, err := d.Canvas.Aspect.Size(); err != nil {
		return issue(FieldCanvas, CodeUnknown)
	}
	ids := map[string]bool{}
	for _, track := range d.Tracks {
		if !validToken(track.ID) || ids[track.ID] || (track.Kind != TrackVisual && track.Kind != TrackAudio) {
			return issue(FieldTracks, CodeInvalid)
		}
		ids[track.ID] = true
		if err := track.validate(d.DurationMS, ids); err != nil {
			return err
		}
	}
	return markersIssue(d.Markers)
}

func markersIssue(markers []Marker) error {
	if len(markers) > MaxMarkers {
		return issue(FieldMarkers, CodeTooMany)
	}
	seen := make(map[string]bool, len(markers))
	for _, m := range markers {
		switch {
		case !validToken(m.ID):
			return issue(FieldMarkers, CodeInvalid)
		case seen[m.ID]:
			return issue(FieldMarkers, CodeDuplicate)
		case m.AtMS < 0 || m.AtMS > mediagen.MaxVideoMS:
			return issue(FieldMarkers, CodeOutOfRange)
		case utf8.RuneCountInString(m.Label) > MaxMarkerLabelRunes:
			return issue(FieldMarkers, CodeTooLarge)
		}
		seen[m.ID] = true
	}
	return nil
}

func (t Track) validate(durationMS int64, ids map[string]bool) error {
	var lastEnd int64
	for i, c := range t.Clips {
		if !validToken(c.ID) || ids[c.ID] {
			return issue(FieldTracks, CodeInvalid)
		}
		ids[c.ID] = true
		if code := c.issue(t.Kind, durationMS); code != "" {
			return issue(FieldTracks, code)
		}
		if i > 0 && c.StartMS < lastEnd {
			return issue(FieldTracks, CodeOverlap)
		}
		lastEnd = c.StartMS + c.DurationMS
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
	}
	if kind == TrackAudio {
		if c.Type != ClipAudio || strings.TrimSpace(c.AssetID) == "" || c.MotionIn != nil || c.MotionOut != nil || c.Keyframes != nil {
			return CodeInvalid
		}
		return ""
	}
	switch c.Type {
	case ClipVideo, ClipImage:
		if strings.TrimSpace(c.AssetID) == "" || (c.Fit != mediagen.FitCover && c.Fit != mediagen.FitContain) {
			return CodeInvalid
		}
	case ClipOverlay:
		if c.Layer == nil || c.AssetID != "" {
			return CodeInvalid
		}
		if code := c.Layer.issue(); code != "" {
			return code
		}
	default:
		return CodeUnknown
	}
	if code := mediagen.MotionIssue(c.MotionIn, c.MotionOut, c.DurationMS); code != "" {
		return code
	}
	if code := mediagen.KeyframesIssue(c.Keyframes, c.Transform); code != "" {
		return code
	}
	return studioTransformIssue(c.Transform)
}

func studioTransformIssue(t mediagen.Transform) string {
	switch {
	case !within(t.X, 0, 1) || !within(t.Y, 0, 1) || !within(t.Opacity, 0, 1):
		return CodeOutOfRange
	case !within(t.W, 0.001, 4) || !within(t.H, 0.001, 4) || !within(t.Rotation, -360, 360):
		return CodeOutOfRange
	}
	return ""
}
