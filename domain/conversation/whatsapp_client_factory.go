package conversation

type WhatsAppClientFactory interface {
	ClientForPhone(businessPhoneID string) (WhatsAppClient, error)

	ClientForWABA(wabaID string) (WhatsAppClient, error)

	WABAIdForPhone(businessPhoneID string) (string, error)
}
