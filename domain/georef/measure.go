package georef

import "vozko/domain/geo"

type SpreadMeasurement struct {
	CEPs       int
	Street     int
	PostalCode int
	City       int
}

func MeasureSpread(points []CEPPoint) SpreadMeasurement {
	var m SpreadMeasurement
	for _, p := range points {
		m.CEPs++
		switch geo.CEPPrecision(p.ZipCode, p.SpreadM) {
		case geo.PrecisionStreet:
			m.Street++
		case geo.PrecisionPostalCode:
			m.PostalCode++
		default:
			m.City++
		}
	}
	return m
}

func (m SpreadMeasurement) StreetShare() float64 {
	if m.CEPs == 0 {
		return 0
	}
	return float64(m.Street) / float64(m.CEPs)
}
