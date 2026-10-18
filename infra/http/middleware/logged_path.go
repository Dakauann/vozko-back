package middleware

import uw "vozko/domain/unofficial_whatsapp"

func LoggedPath(path string) string {
	return uw.RedactWebhookPath(path)
}
