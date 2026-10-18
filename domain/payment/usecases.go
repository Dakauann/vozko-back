package payment

type HandlePaymentWebhookUseCase interface {
	Execute(event *WebhookEvent) error
}

type ConsumePaymentWebhookUseCase interface {
	Start() error
}

type HandleAsaasWebhookUseCase = HandlePaymentWebhookUseCase

type ConsumeAsaasWebhookUseCase = ConsumePaymentWebhookUseCase
