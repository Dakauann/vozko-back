package studio

import (
	"reflect"
	"regexp"
)

type SurfaceKind string

const (
	SurfacePaint SurfaceKind = "paint"
	SurfaceMask  SurfaceKind = "mask"

	PixelStroke = "stroke"
	PixelArea   = "area"

	PixelPaint    = "paint"
	PixelErase    = "erase"
	PixelHide     = "hide"
	PixelReveal   = "reveal"
	PixelSubtract = "subtract"

	MinPixelSide    = 1
	MaxPixelSide    = 4096
	MinPixelSize    = 1.0
	MaxPixelSize    = 1000.0
	MaxPixelFeather = 250.0
	PixelReach      = 8192.0
	minAreaPoints   = 6
	strokeStride    = 2
	pressureStride  = 3
)

var (
	pixelColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	pixelModes        = map[SurfaceKind]map[string]bool{
		SurfacePaint: {PixelPaint: true, PixelErase: true},
		SurfaceMask:  {PixelHide: true, PixelReveal: true},
	}
	paintLayerFields = map[string]bool{
		"id": true, "type": true, "name": true, "transform": true, "hidden": true, "locked": true,
		"groupId": true, "flipX": true, "flipY": true, "blendMode": true, "clip": true, "surface": true,
	}
)

type AreaPart struct {
	Mode   string    `json:"mode,omitempty"`
	Points []float64 `json:"points"`
}

type PixelOp struct {
	Kind     string     `json:"kind"`
	ID       string     `json:"id"`
	Mode     string     `json:"mode"`
	Color    string     `json:"color,omitempty"`
	Size     float64    `json:"size,omitempty"`
	Hardness float64    `json:"hardness,omitempty"`
	Opacity  float64    `json:"opacity"`
	Pressure bool       `json:"pressure,omitempty"`
	Points   []float64  `json:"points,omitempty"`
	Feather  float64    `json:"feather,omitempty"`
	Invert   bool       `json:"invert,omitempty"`
	Parts    []AreaPart `json:"parts,omitempty"`
}

type PixelSurface struct {
	Width  int       `json:"width"`
	Height int       `json:"height"`
	Ops    []PixelOp `json:"ops"`
}

func pixelSideIssue(side int) string {
	if side < MinPixelSide || side > MaxPixelSide {
		return CodeOutOfRange
	}
	return ""
}

func (s PixelSurface) pointsIssue(points []float64, stride int) string {
	for i := 0; i < len(points); i += stride {
		if !within(points[i], -PixelReach, float64(s.Width)+PixelReach) || !within(points[i+1], -PixelReach, float64(s.Height)+PixelReach) {
			return CodeOutOfRange
		}
		if stride == pressureStride && !within(points[i+2], 0, 1) {
			return CodeOutOfRange
		}
	}
	return ""
}

func (op PixelOp) inkIssue(kind SurfaceKind) string {
	if !pixelModes[kind][op.Mode] {
		return CodeUnknown
	}
	if (op.Mode == PixelPaint) != (op.Color != "") {
		return CodeInvalid
	}
	if op.Color != "" && !pixelColorPattern.MatchString(op.Color) {
		return CodeInvalid
	}
	if op.Opacity > 0 && within(op.Opacity, 0, 1) {
		return ""
	}
	return CodeOutOfRange
}

func (op PixelOp) strokeIssue(s PixelSurface) string {
	if op.Feather != 0 || op.Invert || len(op.Parts) > 0 {
		return CodeInvalid
	}
	if !within(op.Size, MinPixelSize, MaxPixelSize) || !within(op.Hardness, 0, 1) {
		return CodeOutOfRange
	}
	stride := strokeStride
	if op.Pressure {
		stride = pressureStride
	}
	if len(op.Points) < stride || len(op.Points)%stride != 0 {
		return CodeInvalid
	}
	return s.pointsIssue(op.Points, stride)
}

func (op PixelOp) areaIssue(s PixelSurface) string {
	if op.Size != 0 || op.Hardness != 0 || op.Pressure || len(op.Points) > 0 {
		return CodeInvalid
	}
	if !within(op.Feather, 0, MaxPixelFeather) {
		return CodeOutOfRange
	}
	if len(op.Parts) == 0 || op.Parts[0].Mode != "" {
		return CodeInvalid
	}
	for _, part := range op.Parts {
		if part.Mode != "" && part.Mode != PixelSubtract {
			return CodeUnknown
		}
		if len(part.Points) < minAreaPoints || len(part.Points)%strokeStride != 0 {
			return CodeInvalid
		}
		if code := s.pointsIssue(part.Points, strokeStride); code != "" {
			return code
		}
	}
	return ""
}

func (op PixelOp) issue(kind SurfaceKind, s PixelSurface) string {
	if !validToken(op.ID) {
		return CodeInvalid
	}
	if op.Kind != PixelStroke && op.Kind != PixelArea {
		return CodeUnknown
	}
	if code := op.inkIssue(kind); code != "" {
		return code
	}
	if op.Kind == PixelStroke {
		return op.strokeIssue(s)
	}
	return op.areaIssue(s)
}

func (s PixelSurface) issue(kind SurfaceKind) string {
	if code := pixelSideIssue(s.Width); code != "" {
		return code
	}
	if code := pixelSideIssue(s.Height); code != "" {
		return code
	}
	seen := make(map[string]bool, len(s.Ops))
	for _, op := range s.Ops {
		if code := op.issue(kind, s); code != "" {
			return code
		}
		if seen[op.ID] {
			return CodeDuplicate
		}
		seen[op.ID] = true
	}
	return ""
}

func (l Layer) setsOnly(allowed map[string]bool) bool {
	value := reflect.ValueOf(l)
	for i := 0; i < value.NumField(); i++ {
		if allowed[jsonName(value.Type().Field(i))] {
			continue
		}
		if field := value.Field(i); !field.IsZero() && !(field.Kind() == reflect.Slice && field.Len() == 0) {
			return false
		}
	}
	return true
}

func (l Layer) pixelIssue() string {
	if l.Type == LayerPaint {
		if l.Surface == nil {
			return CodeRequired
		}
		if !l.setsOnly(paintLayerFields) {
			return CodeInvalid
		}
		return l.Surface.issue(SurfacePaint)
	}
	if l.Surface != nil {
		return CodeInvalid
	}
	if l.Mask == nil {
		return ""
	}
	if l.Type != LayerImage {
		return CodeInvalid
	}
	return l.Mask.issue(SurfaceMask)
}

func (l Layer) carriesPixels() bool {
	return l.Type == LayerPaint || l.Mask != nil || l.Surface != nil
}
