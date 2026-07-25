package affiliate

import "errors"

var (
	ErrAffiliateNotFound        = errors.New("affiliate not found")
	ErrAffiliateAlreadyExists   = errors.New("user is already registered as an affiliate")
	ErrCodeAlreadyTaken         = errors.New("affiliate code already in use")
	ErrInvalidCode              = errors.New("invalid affiliate code")
	ErrInvalidBrandName         = errors.New("invalid brand name")
	ErrInvalidBrandLogoURL      = errors.New("invalid brand logo URL")
	ErrInvalidAsaasWalletID     = errors.New("invalid asaas wallet id")
	ErrWalletValidationFailed   = errors.New("could not validate asaas wallet id with provider")
	ErrInvalidCommissionPct     = errors.New("invalid commission percentage")
	ErrInvalidTier              = errors.New("invalid affiliate tier")
	ErrSelfReferral             = errors.New("users cannot refer their own workspaces")
	ErrWorkspaceAlreadyReferred = errors.New("workspace is already linked to an affiliate")
	ErrUnauthorized             = errors.New("not authorized")
	ErrInvalidReferralCode      = errors.New("referral code does not exist or is inactive")
	ErrExchangeRateUnavailable  = errors.New("global USD→BRL exchange rate is not configured")
)
