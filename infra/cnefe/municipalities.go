package cnefe

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"

	"vozko/domain/address"
	"vozko/domain/georef"
)

var ErrMunicipalitiesEmpty = errors.New("cnefe: the municipality directory is empty or malformed")

type municipalityRow struct {
	ID    int64  `json:"municipio-id"`
	Name  string `json:"municipio-nome"`
	State string `json:"UF-sigla"`
}

func ReadMunicipalities(r io.Reader) (map[string]georef.Municipality, error) {
	var rows []municipalityRow
	if err := json.NewDecoder(r).Decode(&rows); err != nil {
		return nil, fmt.Errorf("cnefe: read municipalities: %w", err)
	}
	out := make(map[string]georef.Municipality, len(rows))
	for _, row := range rows {
		code := strconv.FormatInt(row.ID, 10)
		if !address.ValidCityCode(code) || row.Name == "" {
			return nil, fmt.Errorf("%w: municipality %d", ErrMunicipalitiesEmpty, row.ID)
		}
		out[code] = georef.Municipality{CityCode: code, Name: row.Name, State: row.State}
	}
	if len(out) == 0 {
		return nil, ErrMunicipalitiesEmpty
	}
	return out, nil
}
