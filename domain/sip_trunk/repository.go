package sip_trunk

type Repository interface {
	Create(trunk *SIPTrunk) error
	Update(trunk *SIPTrunk) error
	Delete(id string) error
	FindByID(id string) (*SIPTrunk, error)
	FindByIDs(ids []string) ([]*SIPTrunk, error)
	FindAll(page, pageSize int) ([]*SIPTrunk, int64, error)

	FindAccessible(workspaceID string, extraTrunkIDs []string, page, pageSize int) ([]*SIPTrunk, int64, error)
	FindEnabled() ([]*SIPTrunk, error)
	UpdateStatus(id string, status RegistrationStatus, lastError string) error

	// CountByWorkspace returns how many trunks a workspace owns. Used to enforce
	// the per-workspace self-service trunk cap.
	CountByWorkspace(workspaceID string) (int64, error)
}
