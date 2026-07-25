package auth

const (
	AuthCodeInvalidCredentials = "AUTH_INVALID_CREDENTIALS"
	AuthCodeAccountInactive    = "AUTH_ACCOUNT_INACTIVE"

	AuthCodeEmailAlreadyExists     = "AUTH_EMAIL_ALREADY_EXISTS"
	AuthCodeDocumentAlreadyExists  = "AUTH_DOCUMENT_ALREADY_EXISTS"
	AuthCodeWeakPassword           = "AUTH_WEAK_PASSWORD"
	AuthCodeInvalidCustomerType    = "AUTH_INVALID_CUSTOMER_TYPE"
	AuthCodeMissingDocument        = "AUTH_MISSING_DOCUMENT"
	AuthCodeInvalidDocument        = "AUTH_INVALID_DOCUMENT"
	AuthCodeInvalidName            = "AUTH_INVALID_NAME"
	AuthCodeMissingVerificationTok = "AUTH_MISSING_VERIFICATION_CODE"
	AuthCodeInvalidVerificationTok = "AUTH_INVALID_VERIFICATION_CODE"
	AuthCodeVerificationTokUsed    = "AUTH_VERIFICATION_CODE_USED"

	AuthCodeRateLimitExceeded = "AUTH_RATE_LIMIT_EXCEEDED"
	AuthCodeEmailDeliveryFail = "AUTH_EMAIL_DELIVERY_FAILED"

	AuthCodeInvalidResetToken = "AUTH_INVALID_RESET_TOKEN"
	AuthCodeResetTokenUsed    = "AUTH_RESET_TOKEN_USED"

	AuthCodeInvalidRefreshTok = "AUTH_INVALID_REFRESH_TOKEN"
	AuthCodeTokenRefreshFail  = "AUTH_TOKEN_REFRESH_FAILED"

	AuthCodeValidationFailed = "AUTH_VALIDATION_FAILED"
	AuthCodeInvalidBody      = "AUTH_INVALID_BODY"
	AuthCodeInternal         = "AUTH_INTERNAL_ERROR"
	AuthCodeUnauthorized     = "AUTH_UNAUTHORIZED"
)
