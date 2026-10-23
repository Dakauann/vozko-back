package balance

type ExchangeRate interface {
	CurrentRate() (float64, error)
}
