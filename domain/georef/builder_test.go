package georef

import (
	"math"
	"testing"

	"vozko/domain/geo"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-5 }

func TestLevelFeedsPoints(t *testing.T) {
	tests := []struct {
		level  int
		points bool
		known  bool
	}{
		{1, true, true},
		{2, true, true},
		{3, true, true},
		{4, true, true},
		{5, false, true},
		{6, false, true},
		{0, false, false},
		{7, false, false},
	}
	for _, tt := range tests {
		if got := Level(tt.level).FeedsPoints(); got != tt.points {
			t.Fatalf("Level(%d).FeedsPoints() = %v, expected %v", tt.level, got, tt.points)
		}
		if got := Level(tt.level).Known(); got != tt.known {
			t.Fatalf("Level(%d).Known() = %v, expected %v", tt.level, got, tt.known)
		}
	}
}

func record(city, zip, locality string, lat, lng float64, level int) Record {
	return Record{CityCode: city, ZipCode: zip, Locality: locality, Point: geo.Point{Lat: lat, Lng: lng}, Level: Level(level)}
}

func TestBuilderComputesTheMedianSpreadAndCountPerCEP(t *testing.T) {
	b := NewBuilder(DefaultLimits(), 1)
	rows := []Record{
		record("1400100", "69318240", "CENTRO", 2.8200, -60.7800, 1),
		record("1400100", "69318240", "CENTRO", 2.8210, -60.7810, 2),
		record("1400100", "69318240", "CENTRO", 2.8220, -60.7820, 3),
		record("1400100", "69318240", "CENTRO", 2.8230, -60.7830, 4),
		record("1400100", "69318240", "CENTRO", 2.9000, -60.9000, 6),
		record("1400100", "69318240", "CENTRO", 2.9000, -60.9000, 5),
	}
	for _, r := range rows {
		if !b.Add(r) {
			t.Fatalf("Add(%+v) refused a readable row", r)
		}
	}
	points := b.CEPPoints()
	if len(points) != 1 {
		t.Fatalf("CEPPoints() = %+v, expected one CEP", points)
	}
	p := points[0]
	if p.ZipCode != "69318240" || p.CityCode != "1400100" || p.AddressCount != 6 || p.SampleCount != 4 {
		t.Fatalf("CEP point = %+v, expected 6 addresses counted and 4 pinned samples", p)
	}
	if !near(p.Point.Lat, 2.8215) || !near(p.Point.Lng, -60.7815) {
		t.Fatalf("median = %+v, expected the median of levels 1 to 4 only (2.8215, -60.7815)", p.Point)
	}
	far := geo.DistanceMeters(p.Point, geo.Point{Lat: 2.8230, Lng: -60.7830})
	if math.Abs(p.SpreadM-far) > 1 {
		t.Fatalf("spread = %v, expected the 90th percentile distance %v", p.SpreadM, far)
	}
}

func TestBuilderSkipsCentroidOnlyCEPsButCountsThem(t *testing.T) {
	b := NewBuilder(DefaultLimits(), 1)
	b.Add(record("1400100", "69300000", "CENTRO", 2.8, -60.6, 6))
	b.Add(record("1400100", "69300000", "CENTRO", 2.8, -60.6, 5))
	if got := b.CEPPoints(); len(got) != 0 {
		t.Fatalf("CEPPoints() = %+v, expected no point from tract and locality centroids", got)
	}
	if got := b.Stats(); got.Rows != 2 || got.CountOnly != 2 || got.Pinned != 0 {
		t.Fatalf("Stats() = %+v, expected two count-only rows", got)
	}
}

func TestBuilderRefusesUnreadableRows(t *testing.T) {
	b := NewBuilder(DefaultLimits(), 1)
	bad := []Record{
		record("14001", "69318240", "CENTRO", 2.8, -60.7, 1),
		record("1400100", "6931824", "CENTRO", 2.8, -60.7, 1),
		record("1400100", "69318240", "CENTRO", 0, 0, 1),
		record("1400100", "69318240", "CENTRO", 40.7, -74.0, 1),
		record("1400100", "69318240", "CENTRO", 2.8, -60.7, 9),
	}
	for _, r := range bad {
		if b.Add(r) {
			t.Fatalf("Add(%+v) accepted an unreadable row", r)
		}
	}
	if got := b.Stats(); got.Skipped != len(bad) || got.Rows != 0 {
		t.Fatalf("Stats() = %+v, expected every row skipped", got)
	}
}

func TestBuilderSeedsBairroPointsPerCityAndFoldedName(t *testing.T) {
	b := NewBuilder(DefaultLimits(), 1)
	b.Add(record("1400100", "69318240", "JARDIM FLORESTA", 2.80, -60.70, 1))
	b.Add(record("1400100", "69318241", "Jd. Floresta", 2.82, -60.72, 1))
	b.Add(record("1400100", "69318242", "JARDIM FLORESTA", 2.84, -60.74, 2))
	b.Add(record("1400100", "69318243", "JARDIM FLORESTA", 2.90, -60.90, 6))
	b.Add(record("1400159", "69380000", "JARDIM FLORESTA", 1.00, -61.00, 1))
	b.Add(record("1400159", "69380000", "", 1.10, -61.10, 1))
	districts := b.DistrictPoints()
	if len(districts) != 2 {
		t.Fatalf("DistrictPoints() = %+v, expected one bairro per city", districts)
	}
	d := districts[0]
	if d.CityCode != "1400100" || d.DistrictKey != "jardim floresta" || d.Name != "JARDIM FLORESTA" || d.SampleCount != 3 {
		t.Fatalf("bairro = %+v, expected the folded pair with the most common spelling and 3 pinned samples", d)
	}
	if !near(d.Point.Lat, 2.82) || !near(d.Point.Lng, -60.72) {
		t.Fatalf("bairro median = %+v, expected (2.82, -60.72)", d.Point)
	}
	if districts[1].CityCode != "1400159" {
		t.Fatalf("second bairro = %+v, expected the other city", districts[1])
	}
}

func TestBuilderComputesCityPoints(t *testing.T) {
	b := NewBuilder(DefaultLimits(), 1)
	b.Add(record("1400100", "69318240", "CENTRO", 2.80, -60.70, 1))
	b.Add(record("1400100", "69318241", "CENTRO", 2.82, -60.72, 1))
	b.Add(record("1400100", "69318242", "CENTRO", 2.84, -60.74, 1))
	b.Add(record("1400159", "69380000", "CENTRO", 1.00, -61.00, 5))
	cities := b.CityPoints()
	if len(cities) != 1 {
		t.Fatalf("CityPoints() = %+v, expected only the city with pinned rows", cities)
	}
	if c := cities[0]; c.CityCode != "1400100" || c.SampleCount != 3 || !near(c.Point.Lat, 2.82) || !near(c.Point.Lng, -60.72) {
		t.Fatalf("city = %+v, expected the median of its pinned rows", c)
	}
}

func TestBuilderBoundsTheSamplesItKeeps(t *testing.T) {
	b := NewBuilder(Limits{CEPSample: 8, DistrictSample: 8, CitySample: 8}, 7)
	for i := 0; i < 1000; i++ {
		b.Add(record("1400100", "69318240", "CENTRO", 2.80+float64(i%10)*0.0001, -60.70, 1))
	}
	p := b.CEPPoints()[0]
	if p.AddressCount != 1000 || p.SampleCount != 1000 {
		t.Fatalf("CEP point = %+v, expected every address counted", p)
	}
	if got := len(b.ceps["69318240"].lat); got != 8 {
		t.Fatalf("kept %d samples, expected the limit 8", got)
	}
	if p.Point.Lat < 2.80 || p.Point.Lat > 2.8009 {
		t.Fatalf("median from the sample = %+v, expected inside the points", p.Point)
	}
}

func TestBuilderIsDeterministicForOneSeed(t *testing.T) {
	build := func() CEPPoint {
		b := NewBuilder(Limits{CEPSample: 16, DistrictSample: 16, CitySample: 16}, 42)
		for i := 0; i < 500; i++ {
			b.Add(record("1400100", "69318240", "CENTRO", 2.8+float64(i)*0.00001, -60.7-float64(i%7)*0.00001, 1))
		}
		return b.CEPPoints()[0]
	}
	if a, b := build(), build(); a != b {
		t.Fatalf("two builds with one seed differ: %+v and %+v", a, b)
	}
}

func TestCitiesJoinTheMunicipalityDirectory(t *testing.T) {
	points := []CityPoint{
		{CityCode: "1400100", Point: geo.Point{Lat: 2.82, Lng: -60.67}, SampleCount: 10},
		{CityCode: "1400159", Point: geo.Point{Lat: 1.0, Lng: -61.0}, SampleCount: 3},
	}
	directory := map[string]Municipality{
		"1400100": {CityCode: "1400100", Name: "Boa Vista", State: "RR"},
	}
	cities, missing := Cities(points, directory)
	if len(cities) != 1 || cities[0].Name != "Boa Vista" || cities[0].NameKey != "boa vista" || cities[0].State != "RR" {
		t.Fatalf("Cities() = %+v, expected Boa Vista with its folded name", cities)
	}
	if len(missing) != 1 || missing[0] != "1400159" {
		t.Fatalf("missing = %v, expected the city the directory does not name", missing)
	}
}

func TestMeasureSpread(t *testing.T) {
	points := []CEPPoint{
		{ZipCode: "69318240", SpreadM: 120},
		{ZipCode: "69318241", SpreadM: 299},
		{ZipCode: "69318242", SpreadM: 800},
		{ZipCode: "69300000", SpreadM: 50},
		{ZipCode: "69318243", SpreadM: 9000},
	}
	got := MeasureSpread(points)
	want := SpreadMeasurement{CEPs: 5, Street: 2, PostalCode: 1, City: 2}
	if got != want {
		t.Fatalf("MeasureSpread() = %+v, expected %+v", got, want)
	}
	if share := got.StreetShare(); !near(share, 0.4) {
		t.Fatalf("StreetShare() = %v, expected 0.4", share)
	}
	if (SpreadMeasurement{}).StreetShare() != 0 {
		t.Fatalf("an empty measurement has no share")
	}
}

func TestUFFiles(t *testing.T) {
	files := UFFiles()
	if len(files) != 27 {
		t.Fatalf("UFFiles() has %d entries, expected the 27 units", len(files))
	}
	if files[0].FileName() != "11_RO.zip" {
		t.Fatalf("first file = %q, expected 11_RO.zip", files[0].FileName())
	}
	sp, ok := UFFileFor("sp")
	if !ok || sp.FileName() != "35_SP.zip" {
		t.Fatalf("UFFileFor(sp) = %+v, %v", sp, ok)
	}
	if _, ok := UFFileFor("XX"); ok {
		t.Fatalf("an unknown UF must be refused")
	}
}
