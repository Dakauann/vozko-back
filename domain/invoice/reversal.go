package invoice

import (
	"math"
	"strings"
	"time"
)

const externalReferencePrefix = "inv:"

func ExternalReference(invoiceID string) string {
	return externalReferencePrefix + invoiceID
}

func IDFromExternalReference(ref string) (string, bool) {
	id, found := strings.CutPrefix(ref, externalReferencePrefix)
	if !found || strings.TrimSpace(id) == "" || id != strings.TrimSpace(id) {
		return "", false
	}
	return id, true
}

func ReversalReference(invoiceID string) string {
	return "reversal:" + invoiceID
}

func ReversalTargetMicros(creditedMicros int64, amountBRL, reversedBRL float64) int64 {
	if creditedMicros <= 0 {
		return 0
	}
	if amountBRL <= 0 || reversedBRL <= 0 || reversedBRL >= amountBRL {
		return creditedMicros
	}
	share := int64(math.Round(float64(creditedMicros) * reversedBRL / amountBRL))
	return max(0, min(creditedMicros, share))
}

const ReservationStaleAfter = 15 * time.Minute
