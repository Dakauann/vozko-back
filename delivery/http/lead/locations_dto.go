package lead

type PinLeadLocationRequest struct {
	Latitude  *float64 `json:"latitude" example:"-23.55052"`
	Longitude *float64 `json:"longitude" example:"-46.633308"`
}
