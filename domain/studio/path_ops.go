package studio

import (
	"math"

	"vozko/domain/mediagen"
)

const (
	PathOpOffset       = "offset"
	PathOpPuckerBloat  = "puckerBloat"
	PathOpRoundCorners = "roundCorners"
	PathOpZigZag       = "zigZag"
	PathOpTwist        = "twist"
	PathOpMerge        = "merge"

	MergeUnion     = "union"
	MergeSubtract  = "subtract"
	MergeIntersect = "intersect"
	MergeExclude   = "exclude"

	ZigZagCorner = "corner"
	ZigZagSmooth = "smooth"

	MaxPathOpOffset     = 1000.0
	MaxPuckerBloat      = 100.0
	MaxRoundCorners     = 2000.0
	MaxZigZagSize       = 1000.0
	MaxPathOpAngle      = 3600.0
	MinTwistCenter      = -1.0
	MaxTwistCenter      = 2.0
	MinMergePosition    = -2.0
	MaxMergePosition    = 3.0
	MinMergeSize        = 0.001
	MaxMergeSize        = 8.0
	MinPathOpCopies     = 1
	MinZigZagRidges     = 0
	pathOpFieldAmount   = "amount"
	pathOpFieldCopies   = "copies"
	pathOpFieldCopyStep = "copyOffset"
	pathOpFieldMiter    = "miterLimit"
	pathOpFieldRadius   = "radius"
	pathOpFieldSize     = "size"
	pathOpFieldRidges   = "ridges"
	pathOpFieldAngle    = "angle"
	pathOpFieldCenterX  = "centerX"
	pathOpFieldCenterY  = "centerY"
	pathOpFieldX        = "x"
	pathOpFieldY        = "y"
	pathOpFieldW        = "w"
	pathOpFieldH        = "h"
	pathOpFieldRotation = "rotation"
)

type ValueRange struct {
	Low  float64
	High float64
}

func (r ValueRange) Holds(v float64) bool { return within(v, r.Low, r.High) }

var (
	unbounded    = math.Inf(1)
	mergeModes   = map[string]bool{MergeUnion: true, MergeSubtract: true, MergeIntersect: true, MergeExclude: true}
	zigZagPoints = map[string]bool{ZigZagCorner: true, ZigZagSmooth: true}
	pathOpRanges = map[string]map[string]ValueRange{
		PathOpOffset: {
			pathOpFieldAmount:   {-MaxPathOpOffset, MaxPathOpOffset},
			pathOpFieldCopies:   {MinPathOpCopies, unbounded},
			pathOpFieldCopyStep: {-MaxPathOpOffset, MaxPathOpOffset},
			pathOpFieldMiter:    {MinMiterLimit, MaxMiterLimit},
		},
		PathOpPuckerBloat:  {pathOpFieldAmount: {-MaxPuckerBloat, MaxPuckerBloat}},
		PathOpRoundCorners: {pathOpFieldRadius: {0, MaxRoundCorners}},
		PathOpZigZag: {
			pathOpFieldSize:   {-MaxZigZagSize, MaxZigZagSize},
			pathOpFieldRidges: {MinZigZagRidges, unbounded},
		},
		PathOpTwist: {
			pathOpFieldAngle:   {-MaxPathOpAngle, MaxPathOpAngle},
			pathOpFieldCenterX: {MinTwistCenter, MaxTwistCenter},
			pathOpFieldCenterY: {MinTwistCenter, MaxTwistCenter},
		},
		PathOpMerge: {
			pathOpFieldX:        {MinMergePosition, MaxMergePosition},
			pathOpFieldY:        {MinMergePosition, MaxMergePosition},
			pathOpFieldW:        {MinMergeSize, MaxMergeSize},
			pathOpFieldH:        {MinMergeSize, MaxMergeSize},
			pathOpFieldRotation: {-MaxPathOpAngle, MaxPathOpAngle},
		},
	}
)

type PathOpKeyframes struct {
	Amount     []mediagen.Keyframe `json:"amount,omitempty"`
	Copies     []mediagen.Keyframe `json:"copies,omitempty"`
	CopyOffset []mediagen.Keyframe `json:"copyOffset,omitempty"`
	MiterLimit []mediagen.Keyframe `json:"miterLimit,omitempty"`
	Radius     []mediagen.Keyframe `json:"radius,omitempty"`
	Size       []mediagen.Keyframe `json:"size,omitempty"`
	Ridges     []mediagen.Keyframe `json:"ridges,omitempty"`
	Angle      []mediagen.Keyframe `json:"angle,omitempty"`
	CenterX    []mediagen.Keyframe `json:"centerX,omitempty"`
	CenterY    []mediagen.Keyframe `json:"centerY,omitempty"`
	X          []mediagen.Keyframe `json:"x,omitempty"`
	Y          []mediagen.Keyframe `json:"y,omitempty"`
	W          []mediagen.Keyframe `json:"w,omitempty"`
	H          []mediagen.Keyframe `json:"h,omitempty"`
	Rotation   []mediagen.Keyframe `json:"rotation,omitempty"`
}

type PathOp struct {
	ID         string           `json:"id"`
	Kind       string           `json:"kind"`
	Hidden     bool             `json:"hidden,omitempty"`
	Amount     float64          `json:"amount,omitempty"`
	Copies     int              `json:"copies,omitempty"`
	CopyOffset float64          `json:"copyOffset,omitempty"`
	MiterLimit float64          `json:"miterLimit,omitempty"`
	Radius     float64          `json:"radius,omitempty"`
	Size       float64          `json:"size,omitempty"`
	Ridges     int              `json:"ridges,omitempty"`
	Angle      float64          `json:"angle,omitempty"`
	CenterX    float64          `json:"centerX,omitempty"`
	CenterY    float64          `json:"centerY,omitempty"`
	X          float64          `json:"x,omitempty"`
	Y          float64          `json:"y,omitempty"`
	W          float64          `json:"w,omitempty"`
	H          float64          `json:"h,omitempty"`
	Rotation   float64          `json:"rotation,omitempty"`
	Join       string           `json:"join,omitempty"`
	Points     string           `json:"points,omitempty"`
	Mode       string           `json:"mode,omitempty"`
	Path       string           `json:"path,omitempty"`
	FillRule   string           `json:"fillRule,omitempty"`
	Keyframes  *PathOpKeyframes `json:"keyframes,omitempty"`
}

type pathOpLane struct {
	field  string
	value  float64
	frames []mediagen.Keyframe
}

type pathOpText struct {
	owner string
	value string
}

func PathOpKnown(kind string) bool {
	_, known := pathOpRanges[kind]
	return known
}

func PathOpFieldRange(kind, field string) (ValueRange, bool) {
	r, owned := pathOpRanges[kind][field]
	return r, owned
}

func PathOpJoinKnown(join string) bool { return join != "" && lineJoins[join] }

func (o PathOp) lanes() []pathOpLane {
	k := o.Keyframes
	if k == nil {
		k = &PathOpKeyframes{}
	}
	return []pathOpLane{
		{pathOpFieldAmount, o.Amount, k.Amount},
		{pathOpFieldCopies, float64(o.Copies), k.Copies},
		{pathOpFieldCopyStep, o.CopyOffset, k.CopyOffset},
		{pathOpFieldMiter, o.MiterLimit, k.MiterLimit},
		{pathOpFieldRadius, o.Radius, k.Radius},
		{pathOpFieldSize, o.Size, k.Size},
		{pathOpFieldRidges, float64(o.Ridges), k.Ridges},
		{pathOpFieldAngle, o.Angle, k.Angle},
		{pathOpFieldCenterX, o.CenterX, k.CenterX},
		{pathOpFieldCenterY, o.CenterY, k.CenterY},
		{pathOpFieldX, o.X, k.X},
		{pathOpFieldY, o.Y, k.Y},
		{pathOpFieldW, o.W, k.W},
		{pathOpFieldH, o.H, k.H},
		{pathOpFieldRotation, o.Rotation, k.Rotation},
	}
}

func (o PathOp) keyed() bool {
	for _, lane := range o.lanes() {
		if len(lane.frames) > 0 {
			return true
		}
	}
	return false
}

func (o PathOp) textIssue() string {
	texts := []pathOpText{{PathOpOffset, o.Join}, {PathOpZigZag, o.Points}, {PathOpMerge, o.Mode}, {PathOpMerge, o.Path}, {PathOpMerge, o.FillRule}}
	for _, text := range texts {
		if text.owner != o.Kind && text.value != "" {
			return CodeInvalid
		}
	}
	switch o.Kind {
	case PathOpOffset:
		if !PathOpJoinKnown(o.Join) {
			return CodeUnknown
		}
	case PathOpZigZag:
		if !zigZagPoints[o.Points] {
			return CodeUnknown
		}
	case PathOpMerge:
		if !mergeModes[o.Mode] || !fillRules[o.FillRule] {
			return CodeUnknown
		}
		if !ValidPath(o.Path) {
			return CodeInvalid
		}
	}
	return ""
}

func (o PathOp) issue() string {
	ranges, known := pathOpRanges[o.Kind]
	if !known {
		return CodeUnknown
	}
	if !validToken(o.ID) {
		return CodeInvalid
	}
	if code := o.textIssue(); code != "" {
		return code
	}
	lanes := o.lanes()
	for _, lane := range lanes {
		r, owned := ranges[lane.field]
		if !owned && lane.value != 0 {
			return CodeInvalid
		}
		if owned && !r.Holds(lane.value) {
			return CodeOutOfRange
		}
	}
	for _, lane := range lanes {
		if len(lane.frames) == 0 {
			continue
		}
		r, owned := ranges[lane.field]
		if !owned {
			return CodeInvalid
		}
		if code := keysIssue(lane.frames, r.Low, r.High); code != "" {
			return code
		}
	}
	return ""
}

func (l Layer) takesPathOps() bool {
	return l.Type == LayerShape && l.Shape != "" && !l.strokedShape()
}

func (l Layer) pathOpsKeyed() bool {
	for _, op := range l.PathOps {
		if op.keyed() {
			return true
		}
	}
	return false
}

func (l Layer) pathOpsIssue() string {
	if len(l.PathOps) == 0 {
		return ""
	}
	if !l.takesPathOps() {
		return CodeInvalid
	}
	ids := make(map[string]bool, len(l.PathOps))
	for _, op := range l.PathOps {
		if ids[op.ID] {
			return CodeInvalid
		}
		ids[op.ID] = true
		if code := op.issue(); code != "" {
			return code
		}
	}
	return ""
}

func (l Layer) stillPathOpsIssue() string {
	if l.pathOpsKeyed() {
		return CodeInvalid
	}
	return ""
}
