package georef

import (
	"strings"

	"vozko/domain/address"
	"vozko/domain/geo"
)

type Municipality struct {
	CityCode string
	Name     string
	State    string
}

type City struct {
	CityCode     string
	Name         string
	NameKey      string
	State        string
	Point        geo.Point
	Bounds       geo.BBox
	AddressCount int64
}

func Cities(points []CityPoint, directory map[string]Municipality) ([]City, []string) {
	var cities []City
	var missing []string
	for _, p := range points {
		m, ok := directory[p.CityCode]
		name := strings.TrimSpace(m.Name)
		state, stateOK := address.StateOfCityCode(p.CityCode)
		if !ok || name == "" || !stateOK {
			missing = append(missing, p.CityCode)
			continue
		}
		cities = append(cities, City{
			CityCode: p.CityCode, Name: name, NameKey: address.CityNameKey(name), State: state, Point: p.Point,
			Bounds: p.Bounds, AddressCount: p.SampleCount,
		})
	}
	return cities, missing
}
