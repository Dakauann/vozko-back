package shipping

type Address struct {
	ZipCode    string
	Street     string
	City       string
	State      string
	Country    string
	Number     string
	Complement string
}

type Package struct {
	Weight float64
	Height float64
	Width  float64
	Length float64
	Value  float64
}

type FreightQuote struct {
	ServiceID     string
	ServiceName   string
	Price         float64
	DeliveryDays  int
	EstimatedDate string
	Company       string
}

type ShippingService struct {
	ID          string
	Name        string
	Company     string
	Description string
}

type ShippingLabel struct {
	ID           string
	TrackingCode string
	URL          string
	Price        float64
	ExpiresAt    string
}

type TrackingInfo struct {
	Code              string
	Status            string
	Events            []TrackingEvent
	EstimatedDelivery string
}

type TrackingEvent struct {
	Date        string
	Status      string
	Description string
	Location    string
}
