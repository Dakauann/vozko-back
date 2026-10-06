package studio

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"vozko/domain/mediagen"
)

const (
	SchemaImage         = "studio.image"
	SchemaVideo         = "studio.video"
	DocumentVersion     = 1
	MaxDocumentBytes    = 512 << 10
	MaxLayers           = 200
	MaxTracks           = mediagen.MaxVisualTracks + mediagen.MaxAudioTracks
	MaxClips            = mediagen.MaxTimelineClips
	MinCanvasSide       = 100
	MaxCanvasSide       = 4096
	MaxMarkers          = 50
	MaxMarkerLabelRunes = 64
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
}

type ImageDocument struct {
	Schema  string  `json:"schema"`
	Version int     `json:"version"`
	Canvas  Canvas  `json:"canvas"`
	Layers  []Layer `json:"layers"`
	Groups  []Group `json:"groups,omitempty"`
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
		var doc ImageDocument
		if err := decodeStrict(raw, &doc); err != nil {
			return err
		}
		return doc.validate()
	case KindVideo:
		var doc VideoDocument
		if err := decodeStrict(raw, &doc); err != nil {
			return err
		}
		return doc.validate()
	}
	return issue(FieldKind, CodeUnknown)
}

func (d ImageDocument) validate() error {
	switch {
	case d.Schema != SchemaImage || d.Version != DocumentVersion:
		return issue(FieldDocument, CodeUnknown)
	case d.Canvas.Width < MinCanvasSide || d.Canvas.Width > MaxCanvasSide || d.Canvas.Height < MinCanvasSide || d.Canvas.Height > MaxCanvasSide:
		return issue(FieldCanvas, CodeOutOfRange)
	case !validColor(d.Canvas.Background, false) || !d.Canvas.Gradient.valid():
		return issue(FieldCanvas, CodeInvalid)
	case len(d.Layers) > MaxLayers:
		return issue(FieldLayers, CodeTooMany)
	case len(d.Groups) > MaxLayers:
		return issue(FieldLayers, CodeTooMany)
	}
	if err := layersIssue(d.Layers); err != nil {
		return err
	}
	if code := groupsIssue(d.Groups); code != "" {
		return issue(FieldLayers, code)
	}
	return nil
}

func groupsIssue(groups []Group) string {
	parents := make(map[string]string, len(groups))
	for _, g := range groups {
		if !validToken(g.ID) || (g.ParentID != "" && !validToken(g.ParentID)) || utf8.RuneCountInString(g.Name) > maxTokenRunes*2 {
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

func layersIssue(layers []Layer) error {
	seen := make(map[string]bool, len(layers))
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
	case len(d.Tracks) > MaxTracks:
		return issue(FieldTracks, CodeTooMany)
	case d.DurationMS < 0 || d.DurationMS > mediagen.MaxVideoMS:
		return issue(FieldTracks, CodeOutOfRange)
	}
	if _, err := d.Canvas.Aspect.Size(); err != nil {
		return issue(FieldCanvas, CodeUnknown)
	}
	ids := map[string]bool{}
	clips, keyframes := 0, 0
	kinds := map[TrackKind]int{}
	for _, track := range d.Tracks {
		if !validToken(track.ID) || ids[track.ID] || (track.Kind != TrackVisual && track.Kind != TrackAudio) {
			return issue(FieldTracks, CodeInvalid)
		}
		ids[track.ID] = true
		kinds[track.Kind]++
		clips += len(track.Clips)
		for _, c := range track.Clips {
			keyframes += c.Keyframes.Count()
		}
		if err := track.validate(d.DurationMS, ids); err != nil {
			return err
		}
	}
	if kinds[TrackVisual] > mediagen.MaxVisualTracks || kinds[TrackAudio] > mediagen.MaxAudioTracks {
		return issue(FieldTracks, CodeTooMany)
	}
	if clips > MaxClips || keyframes > mediagen.MaxTimelineKeyframes {
		return issue(FieldTracks, CodeTooMany)
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
