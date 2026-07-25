package businessphone

type OwnerPhone struct {
	ID                 string
	Dialog360ChannelID string
	Dialog360ClientID  string
}

type OwnerPhoneReader interface {
	CountActiveDialog360ByOwner(workspaceID string) (int64, error)
	FindConnectedDialog360ByOwner(workspaceID string) ([]OwnerPhone, error)
	FindSuspendedDialog360ByOwner(workspaceID string) ([]OwnerPhone, error)
	// CountConnectedDialog360GroupedByOwner returns the connected dialog360 number
	// count per owning workspace in a single grouped query. The reconcile job uses
	// it (with batch entitlement resolution) to find the few over-cap workspaces
	// without a per-workspace round trip.
	CountConnectedDialog360GroupedByOwner() (map[string]int, error)
	// ListWorkspaceIDsWithSuspendedDialog360 returns the distinct workspaces that
	// own at least one suspended dialog360 number (the only reactivation candidates).
	ListWorkspaceIDsWithSuspendedDialog360() ([]string, error)
	// ListDialog360ChannelRefs returns every dialog360 phone that carries a partner
	// channel id, with the coordinates and platform-side active flag the vendor
	// reconciliation compares against the partner's ListChannels. It is a single
	// read of the whole dialog360 fleet (a daily job, not a hot path).
	ListDialog360ChannelRefs() ([]Dialog360ChannelRef, error)
}

// Dialog360ChannelRef is a platform-side dialog360 channel with the coordinates the
// vendor reconciliation needs: the partner channel id to match against the partner
// listing, the client-scoped id required to cancel it, the owning workspace, and
// whether the platform still considers it active (CONNECTED) rather than already suspended.
type Dialog360ChannelRef struct {
	PhoneID            string
	Dialog360ChannelID string
	Dialog360ClientID  string
	WorkspaceID        string
	Active             bool
}

type ProvisioningGate interface {
	CanProvisionPhone(workspaceID string) (bool, error)
}

// ChannelStatusReport summarizes one channel-status reconcile pass.
type ChannelStatusReport struct {
	Suspended  int // connected locally but deactivated/gone at the vendor -> SUSPENDED
	Updated    int // metadata backfilled (e.g. the display number that lags channel_live)
	WABAsNamed int // WABA records whose account name was backfilled
}

// ChannelStatusReconciler syncs the local dialog360 fleet with the partner's actual
// channel state. It SUSPENDS numbers whose channel was deactivated/removed on
// 360dialog (so a now-invalid API key is never used again) and backfills the
// metadata that only appears a short while AFTER channel_live — notably the display
// phone number and the WABA account name. Idempotent; safe on webhooks and a timer.
type ChannelStatusReconciler interface {
	Execute() (ChannelStatusReport, error)
}

// EntitlementReconciler re-applies WhatsApp entitlement suspend/reactivate across
// every workspace owning dialog360 numbers. It is the safety net for one-shot
// lapse or purchase events whose suspend/reactivate failed: re-running the
// idempotent handlers converges any number left in the wrong state. Execute
// returns the number of workspaces processed.
type EntitlementReconciler interface {
	Execute() (int, error)
}

// VendorReconcileReport summarizes one pass of the vendor channel reconciliation.
type VendorReconcileReport struct {
	VendorBilling int // partner channels still in a billing state (status live)
	Orphans       int // billing channels with no local record (need manual action)
	Leaks         int // billing channels already suspended locally (a lost cancellation)
	Ownerless     int // billing channels live locally but with no owning workspace (bill for nobody)
	Recancelled   int // leaks/ownerless this pass re-cancelled at the partner
}

// VendorChannelReconciler compares the platform's dialog360 channel state against the
// partner's actual channel listing and converges any divergence that would let
// 360dialog bill a channel the platform is not collecting for. It is the financial
// backstop for a silently lost cancellation: the platform believes a channel cancelled
// while it is still live (and billing) at the vendor. Where the internal
// EntitlementReconciler catches the platform's own view drifting from entitlement, this
// catches the platform's view drifting from the vendor's. Execute returns a report of
// what it found and re-cancelled.
type VendorChannelReconciler interface {
	Execute() (VendorReconcileReport, error)
}
