package property

import "vozko/domain/shared"

type ListPropertiesInput struct {
	Search    string
	Options   shared.QueryOptions
	Latitude  *float64
	Longitude *float64
}

type SearchPropertiesInput struct {
	Query   string
	Options shared.QueryOptions
}
