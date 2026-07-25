package workspace_addon

import "errors"

var (
	ErrAddonNotFound             = errors.New("addon not found")
	ErrAddonSubscriptionNotFound = errors.New("addon subscription not found")
	ErrInvalidAddonKey           = errors.New("addon key is required")
	ErrInvalidAddonName          = errors.New("addon name is required")
	ErrInvalidEntitlementKind    = errors.New("invalid entitlement kind")
	ErrInvalidUnitsPerQuantity   = errors.New("units per quantity must be positive")
	ErrInvalidAddonPrice         = errors.New("addon price and cost must be non-negative")
	ErrInvalidQuantity           = errors.New("quantity must be positive")
	ErrAddonInactive             = errors.New("addon is not available for purchase")
	ErrAddonArchived             = errors.New("addon is archived")
	ErrAddonKeyExists            = errors.New("addon key already exists")
	ErrInvalidAddonSubscription  = errors.New("invalid addon subscription")
	ErrWhatsAppPhoneLimitReached = errors.New("whatsapp business phone limit reached for this workspace")
)
