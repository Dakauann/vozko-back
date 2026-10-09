package leadmap

import (
	"errors"
	"fmt"
	"math"
	"testing"

	"vozko/domain/crmfilter"
	"vozko/domain/customfield"
	"vozko/domain/geo"
)

func TestDefaultViewIsPorBairroWhileFewerThanAFifthPinAHouse(t *testing.T) {
	tests := []struct {
		name    string
		summary Summary
		want    View
	}{
		{"nobody on the map", Summary{Total: 1000, OnMap: 0}, ViewDistricts},
		{"19.9% on the map", Summary{Total: 1000, OnMap: 199}, ViewDistricts},
		{"exactly a fifth on the map", Summary{Total: 1000, OnMap: 200}, ViewPositions},
		{"most on the map", Summary{Total: 1000, OnMap: 900}, ViewPositions},
		{"an empty filter shows positions", Summary{}, ViewPositions},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.summary.DefaultView(); got != tt.want {
				t.Fatalf("DefaultView() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDefaultViewportPrefersLocatedLeadsThenTheCommonestCityThenBrazil(t *testing.T) {
	extent := &geo.BBox{South: -23.60, West: -46.70, North: -23.50, East: -46.60}
	center := geo.Point{Lat: -19.92, Lng: -43.94}
	city := &CityPlace{CityKey: "3106200", Name: "Belo Horizonte", State: "MG", People: 40, Center: &center}
	cityWithoutPoint := &CityPlace{CityKey: "3106200", Name: "Belo Horizonte", State: "MG", People: 40}

	located := DefaultViewport(extent, city, Summary{Total: 100, OnMap: 90})
	if located.Basis != BasisLocated || located.BBox != *extent || located.View != ViewPositions {
		t.Fatalf("located = %+v", located)
	}

	byCity := DefaultViewport(nil, city, Summary{Total: 100})
	if byCity.Basis != BasisCity || byCity.City == nil || byCity.View != ViewDistricts {
		t.Fatalf("by city = %+v", byCity)
	}
	if !holds(byCity.BBox, center) || byCity.BBox.Validate() != nil {
		t.Fatalf("the city box %+v must hold the city point", byCity.BBox)
	}

	country := DefaultViewport(nil, cityWithoutPoint, Summary{Total: 100})
	if country.Basis != BasisCountry || country.BBox != geo.BrazilBounds() {
		t.Fatalf("country = %+v", country)
	}
	if country.City == nil || country.City.Name != "Belo Horizonte" {
		t.Fatal("the commonest city is still named when it has no point yet")
	}

	empty := DefaultViewport(nil, nil, Summary{})
	if empty.Basis != BasisCountry || empty.City != nil {
		t.Fatalf("empty = %+v", empty)
	}
}

func TestDefaultViewportPadsASinglePosition(t *testing.T) {
	one := &geo.BBox{South: -23.55, West: -46.63, North: -23.55, East: -46.63}
	v := DefaultViewport(one, nil, Summary{Total: 1, OnMap: 1})
	if v.Basis != BasisLocated || v.BBox.Validate() != nil {
		t.Fatalf("a single position still opens a valid box: %+v", v.BBox)
	}
	if !holds(v.BBox, geo.Point{Lat: -23.55, Lng: -46.63}) {
		t.Fatalf("the padded box %+v must hold the position", v.BBox)
	}
	broken := &geo.BBox{South: math.NaN(), West: -46.63, North: -23.55, East: -46.63}
	if got := DefaultViewport(broken, nil, Summary{}); got.Basis != BasisCountry {
		t.Fatalf("an unreadable extent falls back, got %+v", got)
	}
}

func classification(sensitive bool) *customfield.Definition {
	return &customfield.Definition{
		Key: "classificacao", ObjectType: customfield.ObjectLead, Type: customfield.TypeSelect,
		Options:     []string{"Positivo", "Negativo", "A conquistar", "Não informado"},
		OptionTones: map[string]customfield.Tone{"Positivo": customfield.ToneChart2, "Negativo": customfield.ToneChart3, "Não informado": customfield.ToneNeutral},
		Sensitive:   sensitive, Role: customfield.RoleClassification,
	}
}

func TestColorByTonesComeFromTheDefinition(t *testing.T) {
	c, err := ColorByFor([]*customfield.Definition{classification(false)}, "classificacao", customfield.Viewer{})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		value string
		want  customfield.Tone
	}{
		{"Positivo", customfield.ToneChart2},
		{"Negativo", customfield.ToneChart3},
		{"A conquistar", customfield.ToneNeutral},
		{"", customfield.ToneNeutral},
		{"Talvez", customfield.ToneNeutral},
	}
	for _, tt := range tests {
		if got := c.ToneOf(tt.value); got != tt.want {
			t.Fatalf("ToneOf(%q) = %q, want %q", tt.value, got, tt.want)
		}
	}
	var none *ColorBy
	if none.ToneOf("Positivo") != customfield.ToneNeutral || none.Key() != "" {
		t.Fatal("no colour-by paints every point neutral")
	}
}

func TestColorByRefusals(t *testing.T) {
	text := &customfield.Definition{Key: "apelido", ObjectType: customfield.ObjectLead, Type: customfield.TypeText}
	deal := classification(false)
	deal.ObjectType = customfield.ObjectOpportunity
	defs := []*customfield.Definition{classification(true), text}
	tests := []struct {
		name   string
		defs   []*customfield.Definition
		key    string
		viewer customfield.Viewer
		want   error
	}{
		{"an unknown key", defs, "partido", customfield.Viewer{ReadsSensitive: true}, ErrColorByInvalid},
		{"a text field has no tones", defs, "apelido", customfield.Viewer{ReadsSensitive: true}, ErrColorByInvalid},
		{"a deal field is not a lead field", []*customfield.Definition{deal}, "classificacao", customfield.Viewer{}, ErrColorByInvalid},
		{"a sensitive field for a viewer who cannot read it", defs, "classificacao", customfield.Viewer{}, ErrColorByForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := ColorByFor(tt.defs, tt.key, tt.viewer); !errors.Is(err, tt.want) {
				t.Fatalf("ColorByFor() = %v, want %v", err, tt.want)
			}
		})
	}
	if c, err := ColorByFor(defs, "  ", customfield.Viewer{}); c != nil || err != nil {
		t.Fatalf("no key means no colour-by, got %+v %v", c, err)
	}
	if c, err := ColorByFor(defs, "classificacao", customfield.Viewer{ReadsSensitive: true}); err != nil || c.Key() != "classificacao" {
		t.Fatalf("a sensitive field for a viewer who reads it = %+v %v", c, err)
	}
}

func TestPointCarriesAtMostFiveLeadsAndAStableID(t *testing.T) {
	ids := []string{"a", "b", "c", "d", "e", "f", "g"}
	p := NewPoint(geo.Point{Lat: -23.5614, Lng: -46.6559}, geo.PrecisionStreet, crmfilter.PlacementOnMap, 7, ids, "Positivo")
	if len(p.LeadIDs) != MaxLeadIDsPerPoint || p.People != 7 {
		t.Fatalf("point = %+v", p)
	}
	if p.ID() != "-23.5614,-46.6559" || p.Tone != customfield.ToneNeutral {
		t.Fatalf("id = %q tone = %q", p.ID(), p.Tone)
	}
	ids[0] = "changed"
	if p.LeadIDs[0] != "a" {
		t.Fatal("a point keeps its own copy of the ids")
	}
}

func TestAnApproximatePointNeverSharesItsIDWithAPreciseOneAtTheSamePosition(t *testing.T) {
	at := geo.Point{Lat: -6.3104, Lng: -35.4793}
	precise := NewPoint(at, geo.PrecisionStreet, crmfilter.PlacementOnMap, 1, []string{"a"}, "")
	approximate := NewPoint(at, geo.PrecisionDistrict, crmfilter.PlacementApproximate, 2, []string{"b", "c"}, "")
	if precise.Placement != crmfilter.PlacementOnMap || approximate.Placement != crmfilter.PlacementApproximate {
		t.Fatalf("placements = %q %q", precise.Placement, approximate.Placement)
	}
	if precise.ID() == approximate.ID() {
		t.Fatalf("both points answer the id %q", precise.ID())
	}
	if approximate.ID() != "approximate:-6.3104,-35.4793" {
		t.Fatalf("approximate id = %q", approximate.ID())
	}
}

func TestAPointIsReadEitherOnTheMapOrApproximate(t *testing.T) {
	tests := []struct {
		raw  string
		want crmfilter.GeoPlacement
		err  error
	}{
		{"", crmfilter.PlacementOnMap, nil},
		{"on_map", crmfilter.PlacementOnMap, nil},
		{" approximate ", crmfilter.PlacementApproximate, nil},
		{"without_address", "", ErrInvalidPlacement},
		{"pending", "", ErrInvalidPlacement},
		{"anywhere", "", ErrInvalidPlacement},
	}
	for _, tt := range tests {
		got, err := PointPlacement(tt.raw)
		if got != tt.want || !errors.Is(err, tt.err) {
			t.Fatalf("PointPlacement(%q) = %q, %v, want %q, %v", tt.raw, got, err, tt.want, tt.err)
		}
	}
}

func TestALayerRequestCarriesTheSwitchAndTheCellSizeOfItsZoom(t *testing.T) {
	w, err := geo.SnapWindow(geo.BBox{South: -24, West: -47, North: -23, East: -46}, 10)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := ColorByFor([]*customfield.Definition{classification(false)}, "classificacao", customfield.Viewer{})
	q := NewLayerRequest(w, c)
	if q.MaxPoints != MaxPoints || q.CellSize != w.CellSize(geo.MaxCellsPerAxis) || q.ColorByKey != "classificacao" || q.Claim != w.Claim() {
		t.Fatalf("request = %+v", q)
	}
	if NewLayerRequest(w, nil).ColorByKey != "" {
		t.Fatal("no colour-by asks for no value")
	}
}

func TestDrawableDistrictsHaveAPositionAndBothKeys(t *testing.T) {
	in := []District{
		{CityKey: "3550308", DistrictKey: "jardim paulista", Name: "Jardim Paulista", Position: geo.Point{Lat: -23.57, Lng: -46.66}, People: 4},
		{CityKey: "3550308", DistrictKey: "centro", Name: "Centro", People: 9},
		{CityKey: "", DistrictKey: "centro", Name: "Centro", Position: geo.Point{Lat: -23.54, Lng: -46.63}, People: 2},
		{CityKey: "3550308", DistrictKey: "se", Name: " ", Position: geo.Point{Lat: -23.55, Lng: -46.63}, People: 2},
	}
	got := Drawable(in)
	if len(got) != 2 || got[0].DistrictKey != "jardim paulista" || got[1].Name != "se" {
		t.Fatalf("drawable = %+v, want the positioned pairs, a blank spelling falling back to the key", got)
	}
}

func TestABairroIsPlacedByPositionsNoCoarserThanTheBairro(t *testing.T) {
	got := DistrictPointPrecisions()
	want := []geo.Precision{geo.PrecisionExact, geo.PrecisionAddress, geo.PrecisionStreet, geo.PrecisionPostalCode, geo.PrecisionDistrict}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("district point precisions = %v, want %v: a city centroid never moves a bairro", got, want)
	}
}

func TestErrorCodesNameEveryMapRefusal(t *testing.T) {
	tests := []struct {
		err  error
		code string
	}{
		{ErrColorByInvalid, "map_color_by_invalid"},
		{ErrColorByForbidden, "map_color_by_forbidden"},
		{ErrInvalidPosition, "map_position_invalid"},
		{ErrInvalidPlacement, "map_placement_invalid"},
		{fmt.Errorf("wrapped: %w", geo.ErrInvalidBBox), "map_window_invalid"},
		{geo.ErrInvalidWindow, "map_window_invalid"},
		{geo.ErrWindowTooWide, "map_window_too_wide"},
		{errors.New("other"), ""},
	}
	for _, tt := range tests {
		if got := ErrorCode(tt.err); got != tt.code {
			t.Fatalf("ErrorCode(%v) = %q, want %q", tt.err, got, tt.code)
		}
	}
}

func TestACellsAnswerNeverHoldsMoreThanTheBoundPerAxis(t *testing.T) {
	w, err := geo.SnapWindow(geo.BBox{South: -23.70, West: -46.80, North: -23.50, East: -46.40}, 14)
	if err != nil {
		t.Fatal(err)
	}
	q := NewLayerRequest(w, nil)
	if (w.BBox.East-w.BBox.West)/q.CellSize > geo.MaxCellsPerAxis || (w.BBox.North-w.BBox.South)/q.CellSize > geo.MaxCellsPerAxis {
		t.Fatalf("cell size %v leaves more than %d cells on an axis of %+v", q.CellSize, geo.MaxCellsPerAxis, w.BBox)
	}
}

func holds(b geo.BBox, p geo.Point) bool {
	return p.Lat >= b.South && p.Lat <= b.North && p.Lng >= b.West && p.Lng <= b.East
}

func TestADistrictNamesItsFilterPair(t *testing.T) {
	d := District{CityKey: "sp:sao paulo", DistrictKey: "jardim paulista"}
	if got := d.Pair(); got != "sp:sao paulo/jardim paulista" {
		t.Fatalf("Pair() = %q, want the district filter value", got)
	}
}

func TestNothingIsLeftOutOfAFilterWithoutAreas(t *testing.T) {
	none := NoneLeftOut()
	if none.Total != 0 || none.Districts == nil || len(none.Districts) != 0 {
		t.Fatalf("NoneLeftOut() = %+v, want zero leads and an empty bairro list", none)
	}
}

func TestATileRequestDrawsPointsUpToTheTileCapAndCellsOfThirtyTwoPerTile(t *testing.T) {
	tile, err := geo.TileAt(12, 1517, 2323)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := ColorByFor([]*customfield.Definition{classification(false)}, "classificacao", customfield.Viewer{})
	q := NewTileRequest(tile, c)
	if q.MaxPoints != MaxTilePoints || q.CellSize != tile.CellSize() || q.ColorByKey != "classificacao" || q.Claim != tile.Claim() {
		t.Fatalf("request = %+v, want the tile cap, its cell size, its claim and the colour-by", q)
	}
	if NewTileRequest(tile, nil).ColorByKey != "" {
		t.Fatal("no colour-by asks for no value")
	}
	if MaxTilePoints != 1000 {
		t.Fatalf("MaxTilePoints = %d, want 1000", MaxTilePoints)
	}
}

func TestAWindowRequestClaimsItsTilesHalfOpen(t *testing.T) {
	w, err := geo.SnapWindow(geo.BBox{South: -24, West: -47, North: -23, East: -46}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if q := NewLayerRequest(w, nil); q.Claim != w.Claim() {
		t.Fatalf("window claim = %+v, want %+v", q.Claim, w.Claim())
	}
}

func TestAnInvalidTileIsCoded(t *testing.T) {
	_, err := geo.TileAt(23, 0, 0)
	if got := ErrorCode(err); got != "map_tile_invalid" {
		t.Fatalf("ErrorCode(%v) = %q, want map_tile_invalid", err, got)
	}
}
