package payment

import (
	"fmt"
	"strings"
	"time"
)

const UnsettledObligationWindow = 60 * 24 * time.Hour

const (
	pixReversalWindow  = 90 * 24 * time.Hour
	cardReversalWindow = 180 * 24 * time.Hour
)

func ReversalWindow(billingType string) time.Duration {
	switch Method(strings.ToUpper(strings.TrimSpace(billingType))) {
	case MethodPix:
		return pixReversalWindow
	case MethodBoleto:
		return 0
	default:
		return cardReversalWindow
	}
}

type Obligations struct {
	Provider   Provider
	Unsettled  int
	Reversible int
}

func (o Obligations) Open() bool {
	return o.Unsettled > 0 || o.Reversible > 0
}

func (o Obligations) String() string {
	return fmt.Sprintf("%s: %d unsettled invoice(s), %d paid invoice(s) still reversible by the payer's bank", o.Provider, o.Unsettled, o.Reversible)
}

func ProvidersMissingCredentials(obligations []Obligations, configured map[Provider]bool) []Obligations {
	var missing []Obligations
	for _, o := range obligations {
		if o.Open() && !configured[o.Provider] {
			missing = append(missing, o)
		}
	}
	return missing
}
