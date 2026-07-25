package callpermission

type Repository interface {
	Upsert(p *CallPermission) error

	FindByPhoneAndNumber(businessPhoneID, userNumber string) (*CallPermission, error)

	FindByPhoneAndLead(businessPhoneID, leadID string) (*CallPermission, error)
}
