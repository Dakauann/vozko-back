package cnefe

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vozko/domain/georef"
)

func fixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "cnefe_fixture.csv"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func buildFrom(t *testing.T, r io.Reader) (*georef.Builder, int64) {
	t.Helper()
	b := georef.NewBuilder(georef.DefaultLimits(), 1)
	reader, err := NewReader(r)
	if err != nil {
		t.Fatalf("NewReader() err = %v", err)
	}
	var lines int64
	for {
		rec, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Next() err = %v", err)
		}
		lines++
		b.Add(rec)
	}
	return b, lines
}

func TestTheFixturePinsTheCNEFEColumns(t *testing.T) {
	b, lines := buildFrom(t, bytes.NewReader(fixture(t)))
	if lines != 19 {
		t.Fatalf("read %d rows, want the 19 data rows of the fixture", lines)
	}
	stats := b.Stats()
	if stats.Rows != 16 || stats.Pinned != 14 || stats.CountOnly != 2 || stats.Skipped != 3 {
		t.Fatalf("Stats() = %+v, want 16 rows (14 pinned, a level 5 and a level 6 counted only) and 3 unreadable", stats)
	}
	ceps := map[string]georef.CEPPoint{}
	for _, p := range b.CEPPoints() {
		ceps[p.ZipCode] = p
	}
	if len(ceps) != 5 {
		t.Fatalf("CEPs = %+v, want 5", ceps)
	}
	satelite := ceps["69318240"]
	if satelite.AddressCount != 6 || satelite.SampleCount != 5 || satelite.CityCode != "1400100" {
		t.Fatalf("69318240 = %+v, want 6 addresses of which 5 feed the point", satelite)
	}
	if satelite.Point.Lat > 2.8203 || satelite.Point.Lat < 2.8201 {
		t.Fatalf("69318240 median = %+v, want it untouched by the level 6 tract centroid", satelite.Point)
	}
	if bonfim := ceps["69380000"]; bonfim.AddressCount != 4 || bonfim.SampleCount != 3 || bonfim.CityCode != "1400159" {
		t.Fatalf("69380000 = %+v, want the level 5 row counted only", bonfim)
	}
	measured := georef.MeasureSpread(b.CEPPoints())
	if measured != (georef.SpreadMeasurement{CEPs: 5, Street: 3, PostalCode: 0, City: 2}) {
		t.Fatalf("MeasureSpread() = %+v, want the two generic 000 CEPs as cities and three street CEPs", measured)
	}
	districts := b.DistrictPoints()
	if len(districts) != 4 {
		t.Fatalf("DistrictPoints() = %+v, want 4 bairro pairs", districts)
	}
	cities := b.CityPoints()
	if len(cities) != 2 || cities[0].SampleCount != 11 || cities[1].SampleCount != 3 {
		t.Fatalf("CityPoints() = %+v, want Boa Vista with 11 pinned rows and Bonfim with 3", cities)
	}
}

func TestReaderRefusesAFileWithoutTheStreetColumns(t *testing.T) {
	_, err := NewReader(strings.NewReader("COD_MUNICIPIO;CEP;DSC_LOCALIDADE;LATITUDE;LONGITUDE;NV_GEO_COORD\r\n"))
	if !errors.Is(err, ErrMissingColumn) {
		t.Fatalf("NewReader() err = %v, want ErrMissingColumn for the street name columns", err)
	}
}

func TestReaderRefusesAFileWithoutTheColumnsItNeeds(t *testing.T) {
	_, err := NewReader(strings.NewReader("COD_MUNICIPIO;CEP;LATITUDE\r\n1400100;69318240;2.8\r\n"))
	if !errors.Is(err, ErrMissingColumn) {
		t.Fatalf("NewReader() err = %v, want ErrMissingColumn", err)
	}
	if _, err := NewReader(strings.NewReader("")); err == nil {
		t.Fatal("an empty file must be refused")
	}
}

func TestReaderDecodesLatin1Localities(t *testing.T) {
	data := "COD_MUNICIPIO;CEP;DSC_LOCALIDADE;NOM_TIPO_SEGLOGR;NOM_TITULO_SEGLOGR;NOM_SEGLOGR;LATITUDE;LONGITUDE;NV_GEO_COORD\r\n" +
		"3550308;01310100;S\xc3O JO\xc3O;AVENIDA;;S\xc3O JO\xc3O;-23.5;-46.6;1\r\n3550308;01310100;SÃO PAULO;RUA;DOUTOR;JOÃO;-23.5;-46.6;1\r\n"
	reader, err := NewReader(strings.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	first, err := reader.Next()
	if err != nil || first.Locality != "SÃO JOÃO" || first.StreetKind != "AVENIDA" || first.StreetTitle != "" || first.StreetName != "SÃO JOÃO" {
		t.Fatalf("Next() = %+v, %v, want the Latin-1 bytes decoded", first, err)
	}
	second, err := reader.Next()
	if err != nil || second.Locality != "SÃO PAULO" || second.StreetKind != "RUA" || second.StreetTitle != "DOUTOR" || second.StreetName != "JOÃO" {
		t.Fatalf("Next() = %+v, %v, want UTF-8 kept", second, err)
	}
}

func TestStreamZipReadsTheCSVInsideTheArchive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "14_RR.zip")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(file)
	entry, err := w.Create("14_RR.csv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(fixture(t)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	file.Close()

	b := georef.NewBuilder(georef.DefaultLimits(), 1)
	rows, err := StreamZip(path, func(r georef.Record) { b.Add(r) })
	if err != nil {
		t.Fatalf("StreamZip() err = %v", err)
	}
	if rows != 19 || len(b.CEPPoints()) != 5 {
		t.Fatalf("StreamZip() read %d rows and %d CEPs, want 19 and 5", rows, len(b.CEPPoints()))
	}
}

func TestStreamZipRefusesAnArchiveWithoutACSV(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.zip")
	file, _ := os.Create(path)
	w := zip.NewWriter(file)
	if _, err := w.Create("readme.txt"); err != nil {
		t.Fatal(err)
	}
	w.Close()
	file.Close()
	if _, err := StreamZip(path, func(georef.Record) {}); !errors.Is(err, ErrNoCSV) {
		t.Fatalf("StreamZip() err = %v, want ErrNoCSV", err)
	}
}
