package billing

// QueryUseCase exposes read access to call billing records to the delivery
// layer, so handlers depend on an application-layer seam rather than the
// repository directly.
type QueryUseCase interface {
	GetByCallID(callID string) (*CallBillingRecord, error)
	GetByCallIDs(callIDs []string) (map[string]*CallBillingRecord, error)
}
