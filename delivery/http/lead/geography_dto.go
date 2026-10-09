package lead

import (
	"vozko/domain/crmfilter"
	"vozko/domain/geo"
	"vozko/domain/leadarea"
	"vozko/domain/leadmap"
	"vozko/domain/shared"
)

type MapSummaryResponse struct {
	Total          int64 `json:"total" example:"1200"`
	OnMap          int64 `json:"onMap" example:"800"`
	Approximate    int64 `json:"approximate" example:"200"`
	WithoutAddress int64 `json:"withoutAddress" example:"150"`
	NotFound       int64 `json:"notFound" example:"30"`
	Pending        int64 `json:"pending" example:"15"`
	QuotaExceeded  int64 `json:"quotaExceeded" example:"5"`
	Refused        int64 `json:"refused" example:"2"`
}

type MapPointResponse struct {
	ID        string   `json:"id" example:"-23.5614,-46.6559"`
	Lat       float64  `json:"lat" example:"-23.5614"`
	Lng       float64  `json:"lng" example:"-46.6559"`
	Precision string   `json:"precision" example:"street" enums:"exact,address,street,postal_code,district,city"`
	Placement string   `json:"placement" example:"on_map" enums:"on_map,approximate"`
	Tone      string   `json:"tone" example:"chart-2" enums:"chart-1,chart-2,chart-3,chart-4,chart-5,neutral"`
	Count     int      `json:"count" example:"3"`
	LeadIDs   []string `json:"leadIds"`
}

type MapCellResponse struct {
	IX        int64   `json:"ix" example:"-4666"`
	IY        int64   `json:"iy" example:"-2356"`
	Placement string  `json:"placement" example:"on_map" enums:"on_map,approximate"`
	Count     int     `json:"count" example:"12"`
	Lat       float64 `json:"lat" example:"-23.555"`
	Lng       float64 `json:"lng" example:"-46.655"`
}

type MapLayerResponse struct {
	Kind            string              `json:"kind" example:"points" enums:"points,cells"`
	Points          *[]MapPointResponse `json:"points,omitempty"`
	CellSizeDegrees *float64            `json:"cellSizeDegrees,omitempty" example:"0.0107"`
	Cells           *[]MapCellResponse  `json:"cells,omitempty"`
}

type MapDistrictResponse struct {
	Pair        string  `json:"pair" example:"sp:sao paulo/jardim paulista"`
	CityKey     string  `json:"cityKey" example:"sp:sao paulo"`
	DistrictKey string  `json:"districtKey" example:"jardim paulista"`
	Name        string  `json:"name" example:"Jardim Paulista"`
	Lat         float64 `json:"lat" example:"-23.57"`
	Lng         float64 `json:"lng" example:"-46.66"`
	Count       int     `json:"count" example:"42"`
}

type MapLeftOutDistrictResponse struct {
	Pair        string `json:"pair" example:"sp:sao paulo/jardim paulista"`
	CityKey     string `json:"cityKey" example:"sp:sao paulo"`
	DistrictKey string `json:"districtKey" example:"jardim paulista"`
	Name        string `json:"name" example:"Jardim Paulista"`
	City        string `json:"city" example:"São Paulo"`
	State       string `json:"state" example:"SP"`
	Count       int64  `json:"count" example:"6"`
}

type MapLeftOutResponse struct {
	Total     int64                        `json:"total" example:"9"`
	Filter    *crmfilter.Filter            `json:"filter,omitempty"`
	Districts []MapLeftOutDistrictResponse `json:"districts"`
}

type MapBBoxResponse struct {
	South float64 `json:"south" example:"-23.6"`
	West  float64 `json:"west" example:"-46.7"`
	North float64 `json:"north" example:"-23.5"`
	East  float64 `json:"east" example:"-46.6"`
}

type MapCityResponse struct {
	CityKey string `json:"cityKey" example:"3550308"`
	Name    string `json:"name" example:"São Paulo"`
	State   string `json:"state" example:"SP"`
	Count   int    `json:"count" example:"70"`
}

type MapViewportResponse struct {
	BBox  MapBBoxResponse  `json:"bbox"`
	Basis string           `json:"basis" example:"located" enums:"area,located,city,country"`
	View  string           `json:"view" example:"positions" enums:"positions,districts"`
	City  *MapCityResponse `json:"city,omitempty"`
}

type MapPeekResponse struct {
	Total int                  `json:"total" example:"7"`
	Items []LeadRecordResponse `json:"items"`
}

type LatLngDTO struct {
	Lat float64 `json:"lat" example:"-23.5614"`
	Lng float64 `json:"lng" example:"-46.6559"`
}

type DrawnAreaShape struct {
	Kind    string      `json:"kind" example:"circle" enums:"polygon,rectangle,circle"`
	Ring    []LatLngDTO `json:"ring,omitempty"`
	Center  *LatLngDTO  `json:"center,omitempty"`
	RadiusM float64     `json:"radiusM,omitempty" example:"1500"`
}

type CreateDrawnAreaRequest struct {
	Name       string          `json:"name" example:"Área desenhada em 08/10/2026 14:05"`
	Visibility string          `json:"visibility,omitempty" example:"private" enums:"private,shared"`
	Shape      *DrawnAreaShape `json:"shape"`
}

type UpdateDrawnAreaRequest struct {
	Name       *string         `json:"name,omitempty"`
	Visibility *string         `json:"visibility,omitempty" enums:"private,shared"`
	Shape      *DrawnAreaShape `json:"shape,omitempty"`
}

type DrawnAreaResponse struct {
	ID         string         `json:"id"`
	Name       string         `json:"name" example:"Raio da escola"`
	Visibility string         `json:"visibility" example:"shared" enums:"private,shared"`
	OwnerID    string         `json:"ownerId"`
	CanEdit    bool           `json:"canEdit"`
	Shape      DrawnAreaShape `json:"shape"`
	CreatedAt  string         `json:"createdAt"`
	UpdatedAt  string         `json:"updatedAt"`
}

type DrawnAreaListResponse struct {
	Items []DrawnAreaResponse `json:"items"`
}

func (s *DrawnAreaShape) toDomain() geo.Shape {
	if s == nil {
		return geo.Shape{}
	}
	out := geo.Shape{Kind: geo.ShapeKind(s.Kind), RadiusM: s.RadiusM}
	for _, p := range s.Ring {
		out.Ring = append(out.Ring, geo.Point{Lat: p.Lat, Lng: p.Lng})
	}
	if s.Center != nil {
		out.Center = geo.Point{Lat: s.Center.Lat, Lng: s.Center.Lng}
	}
	return out
}

func (r CreateDrawnAreaRequest) toDomain() leadarea.Draft {
	return leadarea.Draft{Name: r.Name, Visibility: shared.Visibility(r.Visibility), Shape: r.Shape.toDomain()}
}

func (r UpdateDrawnAreaRequest) toDomain() leadarea.Patch {
	var p leadarea.Patch
	p.Name = r.Name
	if r.Visibility != nil {
		v := shared.Visibility(*r.Visibility)
		p.Visibility = &v
	}
	if r.Shape != nil {
		s := r.Shape.toDomain()
		p.Shape = &s
	}
	return p
}

func drawnShape(s geo.Shape) DrawnAreaShape {
	out := DrawnAreaShape{Kind: string(s.Kind), RadiusM: s.RadiusM}
	for _, p := range s.Ring {
		out.Ring = append(out.Ring, LatLngDTO{Lat: p.Lat, Lng: p.Lng})
	}
	if s.Kind == geo.ShapeCircle {
		out.Center = &LatLngDTO{Lat: s.Center.Lat, Lng: s.Center.Lng}
	}
	return out
}

func drawnAreaResponse(a leadarea.Area, viewerID string) DrawnAreaResponse {
	return DrawnAreaResponse{
		ID:         a.ID,
		Name:       a.Name,
		Visibility: string(a.Visibility),
		OwnerID:    a.OwnerID,
		CanEdit:    a.Owned().CanEdit(viewerID),
		Shape:      drawnShape(a.Shape),
		CreatedAt:  fmtNonZero(a.CreatedAt),
		UpdatedAt:  fmtNonZero(a.UpdatedAt),
	}
}

func mapSummaryResponse(s leadmap.Summary) MapSummaryResponse {
	return MapSummaryResponse(s)
}

func mapLayerResponse(l leadmap.Layer) MapLayerResponse {
	if l.Kind == leadmap.LayerCells {
		cells := make([]MapCellResponse, 0, len(l.Cells))
		for _, c := range l.Cells {
			cells = append(cells, MapCellResponse{IX: c.IX, IY: c.IY, Placement: string(c.Placement), Count: c.People, Lat: c.Center.Lat, Lng: c.Center.Lng})
		}
		size := l.CellSize
		return MapLayerResponse{Kind: string(leadmap.LayerCells), CellSizeDegrees: &size, Cells: &cells}
	}
	points := make([]MapPointResponse, 0, len(l.Points))
	for _, p := range l.Points {
		ids := p.LeadIDs
		if ids == nil {
			ids = []string{}
		}
		points = append(points, MapPointResponse{
			ID: p.ID(), Lat: p.Position.Lat, Lng: p.Position.Lng, Precision: string(p.Precision), Placement: string(p.Placement),
			Tone: string(p.Tone), Count: p.People, LeadIDs: ids,
		})
	}
	return MapLayerResponse{Kind: string(leadmap.LayerPoints), Points: &points}
}

func mapDistrictResponses(districts []leadmap.District) []MapDistrictResponse {
	out := make([]MapDistrictResponse, 0, len(districts))
	for _, d := range districts {
		out = append(out, MapDistrictResponse{Pair: d.Pair(), CityKey: d.CityKey, DistrictKey: d.DistrictKey, Name: d.Name, Lat: d.Position.Lat, Lng: d.Position.Lng, Count: d.People})
	}
	return out
}

func mapViewportResponse(v leadmap.Viewport) MapViewportResponse {
	out := MapViewportResponse{
		BBox:  MapBBoxResponse{South: v.BBox.South, West: v.BBox.West, North: v.BBox.North, East: v.BBox.East},
		Basis: string(v.Basis),
		View:  string(v.View),
	}
	if v.City != nil {
		out.City = &MapCityResponse{CityKey: v.City.CityKey, Name: v.City.Name, State: v.City.State, Count: v.City.People}
	}
	return out
}

func mapLeftOutResponse(l leadmap.LeftOut, link *crmfilter.Filter) MapLeftOutResponse {
	out := MapLeftOutResponse{Total: l.Total, Filter: link, Districts: make([]MapLeftOutDistrictResponse, 0, len(l.Districts))}
	for _, d := range l.Districts {
		out.Districts = append(out.Districts, MapLeftOutDistrictResponse{
			Pair: d.Pair, CityKey: d.CityKey, DistrictKey: d.DistrictKey, Name: d.District, City: d.City, State: d.State, Count: d.Count,
		})
	}
	return out
}
