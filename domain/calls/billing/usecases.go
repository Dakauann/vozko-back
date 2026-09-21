package billing

type QueryUseCase interface {
	GetByCallID(callID string) (*CallBillingRecord, error)
	GetByCallIDs(callIDs []string) (map[string]*CallBillingRecord, error)
}
