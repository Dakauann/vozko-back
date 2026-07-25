package coexistence

type MetaCoexistenceService interface {
	CheckCoexistenceStatus(phoneNumberID, accessToken string) (*PhoneCoexistenceStatus, error)

	InitiateContactsSync(phoneNumberID, accessToken string) (*SyncResponse, error)

	InitiateHistorySync(phoneNumberID, accessToken string) (*SyncResponse, error)
}

type ConsumeCoexistenceWebhookUseCase interface {
	Start() error
}
