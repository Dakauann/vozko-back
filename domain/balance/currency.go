package balance

const LedgerCurrency = "USD"

const BillingCurrency = "BRL"

type ReferenceTotals struct {
	LedgerMicros  int64
	BillingMicros int64
	Transactions  int
	Debits        int
	MissingRate   bool
}

type ReferenceTotaler interface {
	TotalsUnder(workspaceID, referencePrefix string) (ReferenceTotals, error)
}
