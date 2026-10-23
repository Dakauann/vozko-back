package container

import (
	"log"
	"strings"
	"time"

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

	var mpClient mercadopago.Client
	var resolver payment.WebhookResolver
	if c.cfg.MercadoPagoAccessToken != "" {
		mpClient = mercadopago.NewClient(
			c.cfg.MercadoPagoAccessToken,
			c.cfg.MercadoPagoBaseURL,
			mercadopago.WithNotificationURL(c.cfg.MercadoPagoNotificationURL),
		)
		resolver = mercadopago.NewWebhookResolver(mpClient)
	}

	switch c.cfg.PaymentProvider {
	case payment.ProviderMercadoPago:
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

		log.Printf("[container] payment gateway: Mercado Pago in %s mode (split charges unsupported, affiliate and marketplace commissions must be paid out manually)",
			mercadoPagoMode(c.cfg.MercadoPagoAccessToken))
		return asaasSvc, gateway, resolver

	default:
		log.Printf("[container] payment gateway: Asaas")
		if resolver != nil {
			log.Printf("[container] Mercado Pago stays connected for the webhooks of its earlier charges")
		}
		return asaasSvc, asaas_service.NewGateway(asaasSvc), resolver
	}
}

func (c *Container) reachablePaymentProviders() map[payment.Provider]bool {
	return map[payment.Provider]bool{
		payment.ProviderAsaas:       c.cfg.AsaasAPIKey != "" && c.cfg.AsaasWebhookToken != "",
		payment.ProviderMercadoPago: c.cfg.MercadoPagoAccessToken != "" && c.cfg.MercadoPagoWebhookSecret != "",
	}
}

func (c *Container) ensurePaymentProvidersWithObligationsAreReachable() {
	obligations, err := c.repositories.invoice.OpenObligations(time.Now())
	if err != nil {
		log.Fatalf("[payments] cannot read which payment providers still have open charges: %v", err)
	}
	missing := payment.ProvidersMissingCredentials(obligations, c.reachablePaymentProviders())
	if len(missing) == 0 {
		return
	}
	lines := make([]string, 0, len(missing))
	for _, m := range missing {
		lines = append(lines, m.String())
	}
	log.Fatalf("[payments] refusing to start: these providers still have charges whose payments, refunds or chargebacks can arrive, "+
		"but their API key and webhook secret are not configured: %s", strings.Join(lines, "; "))
}

func mercadoPagoMode(accessToken string) string {
	if strings.HasPrefix(accessToken, "TEST-") {
		return "SANDBOX"
	}
	return "PRODUCTION (charges are real)"
}
