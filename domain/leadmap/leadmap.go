package leadmap

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"

	"vozko/domain/crmfilter"
	"vozko/domain/customfield"
	"vozko/domain/geo"
	"vozko/domain/lead"
)

const (
	MaxPoints           = 5000
	MaxTilePoints       = 1000
	MaxLeadIDsPerPoint  = 5
	MaxDistricts        = 2000
	MaxPeekLeads        = 50
	MaxLeftOutDistricts = lead.MaxPlaceDistricts

	districtViewShare  = 0.2
	minimumSpanDegrees = 0.01
	cityHalfSpan       = 0.15
)

var (
	ErrColorByInvalid   = errors.New("leadmap: colour-by must name a lead select field")
	ErrColorByForbidden = errors.New("leadmap: colouring by a sensitive field requires permission to read sensitive data")
	ErrInvalidPosition  = errors.New("leadmap: invalid position")
	ErrInvalidPlacement = errors.New("leadmap: a map point is either on the map or approximate")
)

type Section string

const (
	SectionSummary   Section = "summary"
	SectionLayer     Section = "layer"
	SectionTile      Section = "tile"
	SectionDistricts Section = "districts"
	SectionViewport  Section = "viewport"
	SectionLeftOut   Section = "left_out"
)

type View string

const (
	ViewPositions View = "positions"
	ViewDistricts View = "districts"
)

type Basis string

const (
	BasisLocated Basis = "located"
	BasisCity    Basis = "city"
	BasisCountry Basis = "country"
	BasisArea    Basis = "area"
)

type LayerKind string

const (
	LayerPoints LayerKind = "points"
	LayerCells  LayerKind = "cells"
)

type Summary struct {
	Total          int64
	OnMap          int64
	Approximate    int64
	WithoutAddress int64
	NotFound       int64
	Pending        int64
	QuotaExceeded  int64
	Refused        int64
}

func (s Summary) DefaultView() View {
	if s.Total > 0 && float64(s.OnMap) < districtViewShare*float64(s.Total) {
		return ViewDistricts
	}
	return ViewPositions
}

func DistrictPointPrecisions() []geo.Precision {
	var placing []geo.Precision
	for _, p := range geo.PrecisionsBestFirst() {
		if p == geo.PrecisionDistrict || p.Better(geo.PrecisionDistrict) {
			placing = append(placing, p)
		}
	}
	return placing
}

func PointPlacement(raw string) (crmfilter.GeoPlacement, error) {
	switch p := crmfilter.GeoPlacement(strings.TrimSpace(raw)); p {
	case "":
		return crmfilter.PlacementOnMap, nil
	case crmfilter.PlacementOnMap, crmfilter.PlacementApproximate:
		return p, nil
	}
	return "", ErrInvalidPlacement
}

type Point struct {
	Position  geo.Point
	Precision geo.Precision
	Placement crmfilter.GeoPlacement
	People    int
	LeadIDs   []string
	Value     string
	Tone      customfield.Tone
}

func NewPoint(at geo.Point, precision geo.Precision, placement crmfilter.GeoPlacement, people int, leadIDs []string, value string) Point {
	ids := append([]string(nil), leadIDs[:min(len(leadIDs), MaxLeadIDsPerPoint)]...)
	return Point{Position: at, Precision: precision, Placement: placement, People: people, LeadIDs: ids, Value: value, Tone: customfield.ToneNeutral}
}

func (p Point) ID() string {
	at := strconv.FormatFloat(p.Position.Lat, 'f', -1, 64) + "," + strconv.FormatFloat(p.Position.Lng, 'f', -1, 64)
	if p.Placement == crmfilter.PlacementApproximate {
		return string(crmfilter.PlacementApproximate) + ":" + at
	}
	return at
}

type Cell struct {
	IX, IY    int64
	Placement crmfilter.GeoPlacement
	People    int
	Center    geo.Point
}

type Layer struct {
	Kind     LayerKind
	CellSize float64
	Points   []Point
	Cells    []Cell
}

type LayerRequest struct {
	Claim      geo.Claim
	ColorByKey string
	MaxPoints  int
	CellSize   float64
}

func NewLayerRequest(w geo.Window, colorBy *ColorBy) LayerRequest {
	return LayerRequest{Claim: w.Claim(), ColorByKey: colorBy.Key(), MaxPoints: MaxPoints, CellSize: w.CellSize(geo.MaxCellsPerAxis)}
}

func NewTileRequest(t geo.Tile, colorBy *ColorBy) LayerRequest {
	return LayerRequest{Claim: t.Claim(), ColorByKey: colorBy.Key(), MaxPoints: MaxTilePoints, CellSize: t.CellSize()}
}

type District struct {
	CityKey     string
	DistrictKey string
	Name        string
	Position    geo.Point
	People      int
}

func (d District) Pair() string {
	return crmfilter.DistrictPair(d.CityKey, d.DistrictKey)
}

type LeftOut struct {
	Total     int64
	Districts []lead.DistrictCount
}

func NoneLeftOut() LeftOut {
	return LeftOut{Districts: []lead.DistrictCount{}}
}

func Drawable(districts []District) []District {
	out := make([]District, 0, len(districts))
	for _, d := range districts {
		if strings.TrimSpace(d.CityKey) == "" || strings.TrimSpace(d.DistrictKey) == "" || d.Position.Validate() != nil {
			continue
		}
		if strings.TrimSpace(d.Name) == "" {
			d.Name = d.DistrictKey
		}
		out = append(out, d)
	}
	return out
}

type CityPlace struct {
	CityKey string
	Name    string
	State   string
	People  int
	Center  *geo.Point
}

type Viewport struct {
	BBox  geo.BBox
	Basis Basis
	City  *CityPlace
	View  View
}

func DefaultViewport(extent *geo.BBox, city *CityPlace, s Summary) Viewport {
	view := s.DefaultView()
	if box, ok := padded(extent); ok {
		return Viewport{BBox: box, Basis: BasisLocated, City: city, View: view}
	}
	if city != nil && city.Center != nil && city.Center.Validate() == nil {
		c := *city.Center
		box := geo.BBox{South: c.Lat - cityHalfSpan, West: c.Lng - cityHalfSpan, North: c.Lat + cityHalfSpan, East: c.Lng + cityHalfSpan}
		if box.Validate() == nil {
			return Viewport{BBox: box, Basis: BasisCity, City: city, View: view}
		}
	}
	return Viewport{BBox: geo.BrazilBounds(), Basis: BasisCountry, City: city, View: view}
}

func padded(extent *geo.BBox) (geo.BBox, bool) {
	if extent == nil {
		return geo.BBox{}, false
	}
	b := *extent
	for _, v := range []float64{b.South, b.West, b.North, b.East} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return geo.BBox{}, false
		}
	}
	if b.North-b.South < minimumSpanDegrees {
		mid := (b.North + b.South) / 2
		b.South, b.North = mid-minimumSpanDegrees/2, mid+minimumSpanDegrees/2
	}
	if b.East-b.West < minimumSpanDegrees {
		mid := (b.East + b.West) / 2
		b.West, b.East = mid-minimumSpanDegrees/2, mid+minimumSpanDegrees/2
	}
	if b.Validate() != nil {
		return geo.BBox{}, false
	}
	return b, true
}

type ColorBy struct {
	key   string
	tones map[string]customfield.Tone
}

func ColorByFor(defs []*customfield.Definition, key string, viewer customfield.Viewer) (*ColorBy, error) {
	key = strings.ToLower(strings.TrimSpace(key))
	if key == "" {
		return nil, nil
	}
	for _, def := range defs {
		if def == nil || def.Key != key {
			continue
		}
		if def.ObjectType != customfield.ObjectLead || def.Type != customfield.TypeSelect {
			return nil, ErrColorByInvalid
		}
		if !customfield.VisibleTo(def, viewer) {
			return nil, ErrColorByForbidden
		}
		tones := map[string]customfield.Tone{}
		for _, option := range def.Options {
			if tone, ok := def.OptionTones[option]; ok && tone.Valid() {
				tones[option] = tone
			}
		}
		return &ColorBy{key: key, tones: tones}, nil
	}
	return nil, ErrColorByInvalid
}

func (c *ColorBy) Key() string {
	if c == nil {
		return ""
	}
	return c.key
}

func (c *ColorBy) ToneOf(value string) customfield.Tone {
	if c == nil {
		return customfield.ToneNeutral
	}
	if tone, ok := c.tones[value]; ok {
		return tone
	}
	return customfield.ToneNeutral
}

type Scope = lead.SectionQuery

type Reader interface {
	Summary(ctx context.Context, s Scope) (Summary, error)
	Layer(ctx context.Context, s Scope, q LayerRequest) (Layer, error)
	Districts(ctx context.Context, s Scope, limit int) ([]District, error)
	Extent(ctx context.Context, s Scope) (*geo.BBox, error)
	TopCity(ctx context.Context, s Scope) (*CityPlace, error)
	LeadsAt(ctx context.Context, s Scope, at geo.Point, placement crmfilter.GeoPlacement, limit int) ([]string, int, error)
	LeftOut(ctx context.Context, s Scope, limit int) (LeftOut, error)
}

var errorCodes = []struct {
	err  error
	code string
}{
	{ErrColorByInvalid, "map_color_by_invalid"},
	{ErrColorByForbidden, "map_color_by_forbidden"},
	{ErrInvalidPosition, "map_position_invalid"},
	{ErrInvalidPlacement, "map_placement_invalid"},
	{geo.ErrWindowTooWide, "map_window_too_wide"},
	{geo.ErrInvalidTile, "map_tile_invalid"},
	{geo.ErrInvalidWindow, "map_window_invalid"},
	{geo.ErrInvalidBBox, "map_window_invalid"},
}

func ErrorCode(err error) string {
	for _, known := range errorCodes {
		if errors.Is(err, known.err) {
			return known.code
		}
	}
	return ""
}
