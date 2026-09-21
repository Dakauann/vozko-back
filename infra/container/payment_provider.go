package container

import (
	"log"
	"strings"

	"vozko/domain/payment"
	asaas_service "vozko/infra/asaas"
	"vozko/infra/mercadopago"
)

func (c *Container) buildPaymentProvider() (
	asaas_service.AsaasServiceUseCases,
	payment.Gateway,
	payment.WebhookResolver,
) {
	asaasSvc := asaas_service.NewAsaasService(c.cfg.AsaasAPIKey, c.cfg.AsaasBaseURL)

	switch c.cfg.PaymentProvider {
	case payment.ProviderMercadoPago:
		mpClient := mercadopago.NewClient(
			c.cfg.MercadoPagoAccessToken,
			c.cfg.MercadoPagoBaseURL,
			mercadopago.WithNotificationURL(c.cfg.MercadoPagoNotificationURL),
		)
		isSandboxToken := strings.HasPrefix(c.cfg.MercadoPagoAccessToken, "TEST-")

		sandboxPayer := c.cfg.MercadoPagoSandboxPayerEmail
		if sandboxPayer != "" && !isSandboxToken {
			log.Printf("[container] REFUSING MERCADOPAGO_SANDBOX_PAYER_EMAIL: the access token is not a TEST- token, " +
				"so charges are real and must be addressed to the real customer")
			sandboxPayer = ""
		}

		sandboxStatus := c.cfg.MercadoPagoSandboxPayerStatus
		if sandboxStatus != "" && !isSandboxToken {
			log.Printf("[container] REFUSING MERCADOPAGO_SANDBOX_PAYER_STATUS: the access token is not a TEST- token, " +
				"so charges are real and must carry the real customer's name")
			sandboxStatus = ""
		}

		gateway := mercadopago.NewGateway(mpClient,
			mercadopago.WithSandboxPayerEmail(sandboxPayer),
			mercadopago.WithSandboxPayerStatus(sandboxStatus),
		)
		resolver := mercadopago.NewWebhookResolver(mpClient)

		log.Printf("[container] payment gateway: Mercado Pago in %s mode (split charges unsupported, affiliate and marketplace commissions must be paid out manually)",
			mercadoPagoMode(c.cfg.MercadoPagoAccessToken))
		return asaasSvc, gateway, resolver

	default:
		log.Printf("[container] payment gateway: Asaas")
		return asaasSvc, asaas_service.NewGateway(asaasSvc), nil
	}
}

func mercadoPagoMode(accessToken string) string {
	if strings.HasPrefix(accessToken, "TEST-") {
		return "SANDBOX"
	}
	return "PRODUCTION (charges are real)"
}
