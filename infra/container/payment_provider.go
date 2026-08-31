package container

import (
	"log"
	"strings"

	"vozko/domain/payment"
	asaas_service "vozko/infra/asaas"
	"vozko/infra/mercadopago"
)

// buildPaymentProvider resolves the PAYMENT_PROVIDER switch into concrete adapters.
//
// This is the ONLY place in the codebase that branches on which provider is active.
// Everything downstream — invoices, checkout, the webhook handler — depends on the
// payment.Gateway port and cannot tell the difference.
//
// The Asaas service is built regardless of the selected provider, and deliberately so:
// it also backs affiliate wallet validation, and a deployment that has migrated to
// Mercado Pago still needs to inspect and refund the Asaas charges it issued before the
// switch. When Asaas is not the active provider its credentials are optional, so the
// service is simply unusable rather than absent, and every call it makes fails loudly.
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
		// The sandbox payer override is gated on APP_ENV, which cannot tell a real
		// access token from a test one. Running production credentials on a developer
		// machine is a normal thing to do while validating an integration, and there the
		// override would silently readdress a REAL charge. Refuse that combination
		// rather than trusting the environment name alone.
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

// mercadoPagoMode labels the environment a token bills against, so the boot log states
// plainly whether charges are real. A TEST- prefix is the only reliable signal Mercado
// Pago gives: newer test credentials can also carry the APP_USR- prefix, so anything
// without TEST- is reported as production rather than guessed at.
func mercadoPagoMode(accessToken string) string {
	if strings.HasPrefix(accessToken, "TEST-") {
		return "SANDBOX"
	}
	return "PRODUCTION (charges are real)"
}
