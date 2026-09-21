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
	CountConnectedDialog360GroupedByOwner() (map[string]int, error)
	ListWorkspaceIDsWithSuspendedDialog360() ([]string, error)
	ListDialog360ChannelRefs() ([]Dialog360ChannelRef, error)
}

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

type ChannelStatusReport struct {
	Suspended  int
	Updated    int
	WABAsNamed int
}

type ChannelStatusReconciler interface {
	Execute() (ChannelStatusReport, error)
}

type EntitlementReconciler interface {
	Execute() (int, error)
}

type VendorReconcileReport struct {
	VendorBilling int
	Orphans       int
	Leaks         int
	Ownerless     int
	Recancelled   int
}

type VendorChannelReconciler interface {
	Execute() (VendorReconcileReport, error)
}
