package shipping

type FreightItem struct {
	ID             string
	Width          float64
	Height         float64
	Length         float64
	Weight         float64
	InsuranceValue float64
	Quantity       int
}

type FreightOptions struct {
	InsuranceValue float64
	Receipt        bool
	OwnHand        bool
}

type FreightVolume struct {
	Height float64
	Width  float64
	Length float64
	Weight float64
}

type FreightCalculationRequest struct {
	OriginPostalCode      string
	DestinationPostalCode string
	Items                 []FreightItem
	Package               *Package
	Options               FreightOptions
	Services              []string
	Volumes               []FreightVolume
}
