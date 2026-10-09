package cnefe

import (
	"archive/zip"
	"bufio"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"path"
	"strconv"
	"strings"

	"vozko/domain/geo"
	"vozko/domain/georef"
	"vozko/domain/sheet"
)

const (
	columnCity        = "COD_MUNICIPIO"
	columnZip         = "CEP"
	columnLocality    = "DSC_LOCALIDADE"
	columnStreetKind  = "NOM_TIPO_SEGLOGR"
	columnStreetTitle = "NOM_TITULO_SEGLOGR"
	columnStreetName  = "NOM_SEGLOGR"
	columnLatitude    = "LATITUDE"
	columnLongitude   = "LONGITUDE"
	columnLevel       = "NV_GEO_COORD"
	readBuffer        = 1 << 20
	byteOrderMark     = "\xef\xbb\xbf"
)

var (
	ErrMissingColumn = errors.New("cnefe: the file lacks a column the reference needs")
	ErrNoCSV         = errors.New("cnefe: the archive holds no csv file")
)

var requiredColumns = []string{columnCity, columnZip, columnLocality, columnStreetKind, columnStreetTitle, columnStreetName, columnLatitude, columnLongitude, columnLevel}

type Reader struct {
	csv     *csv.Reader
	columns map[string]int
	width   int
}

func NewReader(r io.Reader) (*Reader, error) {
	reader := csv.NewReader(bufio.NewReaderSize(r, readBuffer))
	reader.Comma = ';'
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1
	reader.ReuseRecord = true
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("cnefe: read header: %w", err)
	}
	columns := make(map[string]int, len(header))
	for i, name := range header {
		columns[strings.ToUpper(strings.TrimSpace(strings.TrimPrefix(name, byteOrderMark)))] = i
	}
	width := 0
	for _, name := range requiredColumns {
		index, ok := columns[name]
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrMissingColumn, name)
		}
		width = max(width, index+1)
	}
	return &Reader{csv: reader, columns: columns, width: width}, nil
}

func (r *Reader) Next() (georef.Record, error) {
	for {
		fields, err := r.csv.Read()
		if err != nil {
			return georef.Record{}, err
		}
		if len(fields) == 1 && strings.TrimSpace(fields[0]) == "" {
			continue
		}
		if len(fields) < r.width {
			return georef.Record{}, nil
		}
		return georef.Record{
			CityCode:    strings.TrimSpace(r.field(fields, columnCity)),
			ZipCode:     strings.TrimSpace(r.field(fields, columnZip)),
			Locality:    text(r.field(fields, columnLocality)),
			StreetKind:  text(r.field(fields, columnStreetKind)),
			StreetTitle: text(r.field(fields, columnStreetTitle)),
			StreetName:  text(r.field(fields, columnStreetName)),
			Point:       geo.Point{Lat: number(r.field(fields, columnLatitude)), Lng: number(r.field(fields, columnLongitude))},
			Level:       georef.Level(integer(r.field(fields, columnLevel))),
		}, nil
	}
}

func (r *Reader) field(fields []string, column string) string {
	return fields[r.columns[column]]
}

func number(raw string) float64 {
	value, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(raw), ",", "."), 64)
	if err != nil {
		return math.NaN()
	}
	return value
}

func integer(raw string) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0
	}
	return value
}

func text(raw string) string {
	return sheet.DecodeLegacyText(strings.TrimSpace(raw))
}

func StreamZip(zipPath string, each func(georef.Record)) (int64, error) {
	archive, err := zip.OpenReader(zipPath)
	if err != nil {
		return 0, fmt.Errorf("cnefe: open %s: %w", zipPath, err)
	}
	defer archive.Close()
	for _, file := range archive.File {
		if !strings.EqualFold(path.Ext(file.Name), ".csv") {
			continue
		}
		body, err := file.Open()
		if err != nil {
			return 0, fmt.Errorf("cnefe: open %s in %s: %w", file.Name, zipPath, err)
		}
		defer body.Close()
		return stream(body, each)
	}
	return 0, fmt.Errorf("%w: %s", ErrNoCSV, zipPath)
}

func stream(r io.Reader, each func(georef.Record)) (int64, error) {
	reader, err := NewReader(r)
	if err != nil {
		return 0, err
	}
	var rows int64
	for {
		rec, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return rows, nil
		}
		if err != nil {
			return rows, fmt.Errorf("cnefe: row %d: %w", rows+2, err)
		}
		rows++
		each(rec)
	}
}
