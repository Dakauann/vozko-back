package notification

type ConsumeEmailUseCase interface {
	Start() error
}

type PublishEmailUseCase interface {
	Publish(userEmail string, subject string, template string, placeholders map[string]interface{}) error
}
