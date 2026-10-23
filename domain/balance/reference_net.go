package balance

type ReferenceNetReader interface {
	NetCreditUnder(workspaceID, referenceID string) (int64, error)
}
